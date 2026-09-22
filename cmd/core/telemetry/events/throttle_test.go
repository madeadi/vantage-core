package events

import (
	"sync"
	"testing"
	"time"

	"vantageos-core/cmd/core/telemetry/validate"
)

type emittedEvent struct {
	violation validate.Violation
	count     int
	firstSeen time.Time
	lastSeen  time.Time
}

type fakeSink struct {
	mu     sync.Mutex
	events []emittedEvent
}

func (f *fakeSink) Emit(v validate.Violation, count int, firstSeen, lastSeen time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, emittedEvent{violation: v, count: count, firstSeen: firstSeen, lastSeen: lastSeen})
}

func (f *fakeSink) snapshot() []emittedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]emittedEvent, len(f.events))
	copy(out, f.events)
	return out
}

func waitForEventCount(t *testing.T, sink *fakeSink, want int, timeout time.Duration) []emittedEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		events := sink.snapshot()
		if len(events) >= want {
			return events
		}
		select {
		case <-deadline:
			t.Fatalf("got %d events after %v, want at least %d", len(events), timeout, want)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func testViolation(agentID string) validate.Violation {
	return validate.Violation{
		AgentID: agentID,
		Kind:    validate.KindMissingRequired,
		Paths:   []string{"/status"},
		Detail:  "missing required field(s): status",
	}
}

// TestBurstOfIdenticalViolationsProducesAtMostTwoRows is spec Step 9's
// literal "Done when": a 10k-bad-message burst produces <=2 rows with a
// correct count.
func TestBurstOfIdenticalViolationsProducesAtMostTwoRows(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: 50 * time.Millisecond, PerAgentCap: 50})

	const burst = 10000
	v := testViolation("bad-robot")
	for i := 0; i < burst; i++ {
		th.Record(v)
	}

	// The first occurrence emits synchronously inside Record, so it's
	// already in the sink; the summary emits later, off the window timer.
	events := waitForEventCount(t, sink, 2, time.Second)
	if len(events) > 2 {
		t.Fatalf("got %d events for a %d-message burst of one signature, want <=2", len(events), burst)
	}

	first, summary := events[0], events[1]
	if first.count != 1 {
		t.Errorf("first event count = %d, want 1", first.count)
	}
	if summary.count != burst {
		t.Errorf("summary count = %d, want %d", summary.count, burst)
	}
	if summary.violation.Signature() != first.violation.Signature() {
		t.Error("summary and first event have different signatures for what should be the same underlying violation")
	}
}

func TestFirstOccurrenceEmitsImmediately(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 50}) // window long enough it will not close during the test

	th.Record(testViolation("smallbot"))

	events := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events immediately after the first Record, want 1", len(events))
	}
	if events[0].count != 1 {
		t.Errorf("count = %d, want 1", events[0].count)
	}
}

func TestNoSummaryWhenOnlyOneOccurrence(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: 20 * time.Millisecond, PerAgentCap: 50})

	th.Record(testViolation("smallbot")) // exactly one occurrence, ever

	time.Sleep(100 * time.Millisecond) // let the window close
	events := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events for a signature seen exactly once, want 1 (no summary)", len(events))
	}
}

func TestDistinctSignaturesEachEmitIndependently(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 50})

	a := testViolation("smallbot")
	b := validate.Violation{AgentID: "smallbot", Kind: validate.KindTypeMismatch, Paths: []string{"/battery_percent"}}

	th.Record(a)
	th.Record(b)

	events := sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events for two distinct signatures, want 2", len(events))
	}
}

func TestDistinctAgentsSameSignatureEachEmitIndependently(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 50})

	th.Record(testViolation("robot-a"))
	th.Record(testViolation("robot-a")) // suppressed -- second occurrence, same agent
	th.Record(testViolation("robot-b")) // different agent, same signature -- must emit

	events := sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d immediate events, want 2 (one per agent's first occurrence)", len(events))
	}
}

func TestPerAgentLRUCapEvictsOldestAndReportsIt(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 2})

	v1 := validate.Violation{AgentID: "smallbot", Kind: validate.KindMissingRequired, Paths: []string{"/a"}}
	v2 := validate.Violation{AgentID: "smallbot", Kind: validate.KindMissingRequired, Paths: []string{"/b"}}
	v3 := validate.Violation{AgentID: "smallbot", Kind: validate.KindMissingRequired, Paths: []string{"/c"}}

	th.Record(v1) // oldest -- never touched again, so it's the LRU victim once the cap is exceeded
	th.Record(v2)
	th.Record(v3) // cap is 2 -- forces v1's window closed early

	stats := th.Stats()
	if stats.Evictions != 1 {
		t.Errorf("Evictions = %d, want 1", stats.Evictions)
	}
	if stats.ActiveWindows > 2 {
		t.Errorf("ActiveWindows = %d, want <=2 (cap)", stats.ActiveWindows)
	}

	// v1 was seen exactly once before its window was evicted -- its
	// eviction must not fabricate a summary for a count of 1 (see
	// TestEvictedWindowWithSuppressedOccurrencesEmitsSummaryOnEviction for
	// the case where eviction does owe a summary).
	events := sink.snapshot()
	var v1Count int
	for _, e := range events {
		if e.violation.Signature() == v1.Signature() {
			v1Count++
		}
	}
	if v1Count != 1 {
		t.Errorf("v1 (seen once, then evicted) appears %d times in the sink, want 1 (no summary owed for a single occurrence)", v1Count)
	}
}

func TestEvictedWindowWithSuppressedOccurrencesEmitsSummaryOnEviction(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 1})

	v1 := validate.Violation{AgentID: "smallbot", Kind: validate.KindMissingRequired, Paths: []string{"/a"}}
	v2 := validate.Violation{AgentID: "smallbot", Kind: validate.KindMissingRequired, Paths: []string{"/b"}}

	th.Record(v1)
	th.Record(v1) // suppressed, count=2 for v1's window
	th.Record(v1) // suppressed, count=3
	th.Record(v2) // cap=1 -- evicts v1's window mid-count, must not lose that count=3

	events := sink.snapshot()
	var sawV1Summary bool
	for _, e := range events {
		if e.violation.Signature() == v1.Signature() && e.count == 3 {
			sawV1Summary = true
		}
	}
	if !sawV1Summary {
		t.Errorf("eviction of a window with 3 accumulated occurrences did not emit its summary; got events: %+v", events)
	}
}

// distinctViolation returns a violation whose Signature() is unique to n --
// Signature() deliberately excludes Detail (see its doc comment: it varies
// per occurrence of the same underlying bug), so tests that need N
// genuinely distinct signatures must vary Paths, not Detail.
func distinctViolation(agentID string, n int) validate.Violation {
	return validate.Violation{
		AgentID: agentID,
		Kind:    validate.KindMissingRequired,
		Paths:   []string{"/field" + string(rune('a'+n%26)) + string(rune('a'+(n/26)%26))},
	}
}

func TestGlobalRateCeilingDropsExcessAndCounts(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 100, GlobalPerSecond: 3})

	for i := 0; i < 10; i++ {
		th.Record(distinctViolation("smallbot", i)) // 10 distinct signatures, each a legitimate first occurrence
	}

	events := sink.snapshot()
	if len(events) > 3 {
		t.Errorf("got %d events with a global ceiling of 3/s, want <=3 within the same second", len(events))
	}
	if th.Stats().RateDropped == 0 {
		t.Error("RateDropped = 0, want >0 after exceeding the global ceiling")
	}
}

func TestGlobalCeilingDisabledByDefault(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{Window: time.Hour, PerAgentCap: 1000}) // GlobalPerSecond left at zero value

	const n = 200
	for i := 0; i < n; i++ {
		th.Record(distinctViolation("smallbot", i))
	}

	if got := len(sink.snapshot()); got != n {
		t.Errorf("got %d events with no configured ceiling, want %d (all distinct signatures, none dropped)", got, n)
	}
	if th.Stats().RateDropped != 0 {
		t.Errorf("RateDropped = %d, want 0 when GlobalPerSecond is unset", th.Stats().RateDropped)
	}
}

func TestDefaultsApplyWhenConfigZero(t *testing.T) {
	sink := &fakeSink{}
	th := New(sink, Config{}) // everything zero -- must fall back to spec defaults

	if th.window != 60*time.Second {
		t.Errorf("default window = %v, want 60s", th.window)
	}
	if th.perAgentCap != 50 {
		t.Errorf("default perAgentCap = %d, want 50", th.perAgentCap)
	}
}
