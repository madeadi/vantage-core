// Package events throttles repeated telemetry violations so a misbehaving
// agent cannot flood the database, and persists the survivors. See
// specs/mqtt_telemetry.specs.md Step 9.
package events

import (
	"container/list"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"vantageos-core/cmd/core/telemetry/validate"
)

// TypeTelemetryViolation is the agent_events.type value every Violation this
// package emits is stored under. The field is free-text (see the
// agent_events migration's own doc comment) so future event kinds -- e.g.
// presence changes in spec Step 16 -- can share the collection without a
// schema change.
const TypeTelemetryViolation = "telemetry_violation"

// Sink persists one throttled event, typically as a row in the agent_events
// PocketBase collection (see PocketBaseSink). Implementations should not
// block for long: Emit is called from the Throttler's own goroutines,
// including timer callbacks closing other agents' windows.
type Sink interface {
	Emit(v validate.Violation, count int, firstSeen, lastSeen time.Time)
}

// window is one (agent_id, signature)'s open suppression window.
type window struct {
	violation validate.Violation // the first occurrence -- Detail/Sample are its, even in the closing summary
	count     int
	firstSeen time.Time
	lastSeen  time.Time
	timer     *time.Timer
	lruElem   *list.Element // this window's node in its agent's LRU list
}

// Throttler collapses repeated identical violations into two events instead
// of one-per-occurrence: the first occurrence emits immediately, everything
// else for the same (agent_id, signature) within the window is counted and
// suppressed, and one summary emits when the window closes -- see Record.
//
// Memory is bounded two ways: an LRU cap on open windows per agent (an
// agent emitting endless *distinct* violations must not grow memory
// without limit -- perAgentCap forces the oldest window for that agent
// closed, summary emitted, to make room), and an optional global
// events/sec ceiling across all agents (globalPerSecond; 0 disables it).
// Safe for concurrent use -- required, not just defensive: window-close
// runs on time.AfterFunc's own goroutine, concurrently with Record.
type Throttler struct {
	sink            Sink
	window          time.Duration
	perAgentCap     int
	globalPerSecond int

	mu       sync.Mutex
	byAgent  map[string]map[string]*window // agent_id -> signature -> window
	lruLists map[string]*list.List         // agent_id -> signatures, most-recently-used at Front

	evictions atomic.Uint64
	dropped   atomic.Uint64 // dropped by the global rate ceiling

	rateMu     sync.Mutex
	rateSecond int64
	rateCount  int
}

// Config configures a Throttler. Zero values fall back to the documented
// defaults from specs/mqtt_telemetry.specs.md.
type Config struct {
	// Window is how long a signature's occurrences are suppressed and
	// counted after its first emission. Default 60s.
	Window time.Duration
	// PerAgentCap is the maximum number of distinct open windows (distinct
	// signatures) tracked per agent at once. Default 50.
	PerAgentCap int
	// GlobalPerSecond caps total Emit calls per second across every agent.
	// 0 (the zero value) disables the ceiling.
	GlobalPerSecond int
}

// New returns a Throttler emitting through sink.
func New(sink Sink, cfg Config) *Throttler {
	if cfg.Window <= 0 {
		cfg.Window = 60 * time.Second
	}
	if cfg.PerAgentCap <= 0 {
		cfg.PerAgentCap = 50
	}
	return &Throttler{
		sink:            sink,
		window:          cfg.Window,
		perAgentCap:     cfg.PerAgentCap,
		globalPerSecond: cfg.GlobalPerSecond,
		byAgent:         make(map[string]map[string]*window),
		lruLists:        make(map[string]*list.List),
	}
}

// Record processes one violation: emits immediately if this is the first
// occurrence of (v.AgentID, v.Signature()) within the current window, or
// silently counts it against that window's total otherwise.
func (t *Throttler) Record(v validate.Violation) {
	sig := v.Signature()
	now := time.Now()

	t.mu.Lock()

	agentWindows, ok := t.byAgent[v.AgentID]
	if !ok {
		agentWindows = make(map[string]*window)
		t.byAgent[v.AgentID] = agentWindows
		t.lruLists[v.AgentID] = list.New()
	}
	lru := t.lruLists[v.AgentID]

	if w, exists := agentWindows[sig]; exists {
		w.count++
		w.lastSeen = now
		lru.MoveToFront(w.lruElem)
		t.mu.Unlock()
		return
	}

	w := &window{violation: v, count: 1, firstSeen: now, lastSeen: now}
	w.lruElem = lru.PushFront(sig)
	agentWindows[sig] = w
	w.timer = time.AfterFunc(t.window, func() { t.closeWindow(v.AgentID, sig) })

	var evicted *window
	if lru.Len() > t.perAgentCap {
		evicted = t.evictOldest(v.AgentID, lru)
	}

	t.mu.Unlock()

	t.tryEmit(v, 1, now, now)
	if evicted != nil {
		t.evictions.Add(1)
		if evicted.count > 1 {
			t.tryEmit(evicted.violation, evicted.count, evicted.firstSeen, evicted.lastSeen)
		}
	}
}

// evictOldest removes and returns the least-recently-used window for
// agentID. Caller must hold t.mu.
func (t *Throttler) evictOldest(agentID string, lru *list.List) *window {
	back := lru.Back()
	sig := back.Value.(string)
	lru.Remove(back)

	agentWindows := t.byAgent[agentID]
	w := agentWindows[sig]
	delete(agentWindows, sig)
	w.timer.Stop()
	return w
}

// closeWindow ends the suppression window for (agentID, sig): if anything
// beyond the first occurrence arrived, emits one summary carrying the
// accumulated count; if nothing did (count == 1), the already-emitted
// first-occurrence event is the whole story and nothing further emits.
func (t *Throttler) closeWindow(agentID, sig string) {
	t.mu.Lock()
	agentWindows, ok := t.byAgent[agentID]
	if !ok {
		t.mu.Unlock()
		return // agent's last window was already evicted/closed
	}
	w, ok := agentWindows[sig]
	if !ok {
		t.mu.Unlock()
		return // already evicted
	}

	delete(agentWindows, sig)
	if len(agentWindows) == 0 {
		delete(t.byAgent, agentID)
		delete(t.lruLists, agentID)
	} else {
		t.lruLists[agentID].Remove(w.lruElem)
	}
	t.mu.Unlock()

	if w.count > 1 {
		t.tryEmit(w.violation, w.count, w.firstSeen, w.lastSeen)
	}
}

// tryEmit applies the global rate ceiling (if configured) before calling
// sink.Emit, counting anything it drops.
func (t *Throttler) tryEmit(v validate.Violation, count int, firstSeen, lastSeen time.Time) {
	if t.globalPerSecond > 0 {
		nowSecond := time.Now().Unix()
		t.rateMu.Lock()
		if nowSecond != t.rateSecond {
			t.rateSecond = nowSecond
			t.rateCount = 0
		}
		if t.rateCount >= t.globalPerSecond {
			t.rateMu.Unlock()
			t.dropped.Add(1)
			slog.Warn("telemetry events: global rate ceiling hit, dropping event",
				"agent_id", v.AgentID, "kind", v.Kind, "ceiling", t.globalPerSecond)
			return
		}
		t.rateCount++
		t.rateMu.Unlock()
	}

	t.sink.Emit(v, count, firstSeen, lastSeen)
}

// Stats is a snapshot of the throttler's counters.
type Stats struct {
	Evictions     uint64
	RateDropped   uint64
	ActiveAgents  int
	ActiveWindows int
}

// Stats returns a snapshot of the throttler's counters.
func (t *Throttler) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()

	windows := 0
	for _, m := range t.byAgent {
		windows += len(m)
	}
	return Stats{
		Evictions:     t.evictions.Load(),
		RateDropped:   t.dropped.Load(),
		ActiveAgents:  len(t.byAgent),
		ActiveWindows: windows,
	}
}
