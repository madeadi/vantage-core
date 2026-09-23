package live

import (
	"testing"
	"time"
)

func TestSubscribeReceivesPublishedPayload(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("agent-1")
	defer unsubscribe()

	b.Publish("agent-1", []byte(`{"x":1}`))

	select {
	case got := <-ch:
		if string(got) != `{"x":1}` {
			t.Errorf("got %s, want {\"x\":1}", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never received the published payload")
	}
}

func TestPublishToUnrelatedAgentDoesNotDeliver(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("agent-1")
	defer unsubscribe()

	b.Publish("agent-2", []byte(`{"x":1}`))

	select {
	case got := <-ch:
		t.Fatalf("subscriber to agent-1 received a message published to agent-2: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestPublishNeverBlocksOnASlowSubscriber is the core correctness property:
// Publish is called from the ingest daemon's own single consumer goroutine
// (see ingest.Listener's doc comment), so a subscriber that never reads must
// not be able to stall it.
func TestPublishNeverBlocksOnASlowSubscriber(t *testing.T) {
	b := New()
	_, unsubscribe := b.Subscribe("agent-1") // never read from
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish("agent-1", []byte(`{"x":1}`))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a subscriber that never read from its channel")
	}
}

// TestSubscriberSeesLatestValueNotStale proves the drop-oldest-in-favor-of-
// newest behavior documented on Publish: a subscriber that reads late gets
// the most recent update, not a queue of every update since it last read.
func TestSubscriberSeesLatestValueNotStale(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("agent-1")
	defer unsubscribe()

	b.Publish("agent-1", []byte(`{"seq":1}`))
	b.Publish("agent-1", []byte(`{"seq":2}`))
	b.Publish("agent-1", []byte(`{"seq":3}`))

	select {
	case got := <-ch:
		if string(got) != `{"seq":3}` {
			t.Errorf("got %s, want the latest published value {\"seq\":3}", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never received anything")
	}

	select {
	case got := <-ch:
		t.Fatalf("subscriber received a second message %s -- only the latest should have survived", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("agent-1")
	unsubscribe()

	b.Publish("agent-1", []byte(`{"x":1}`))

	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("received %s after unsubscribing", got)
		}
	case <-time.After(50 * time.Millisecond):
		// Channel not closed (Subscribe doesn't promise that), but also no
		// delivery -- either is acceptable, a read attempt just must not
		// receive a post-unsubscribe publish.
	}

	if len(b.subs) != 0 {
		t.Errorf("Broadcaster still tracks %d agent(s) after the only subscriber unsubscribed", len(b.subs))
	}
}

func TestMultipleSubscribersToSameAgentAllReceive(t *testing.T) {
	b := New()
	ch1, unsub1 := b.Subscribe("agent-1")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("agent-1")
	defer unsub2()

	b.Publish("agent-1", []byte(`{"x":1}`))

	for i, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != `{"x":1}` {
				t.Errorf("subscriber %d: got %s, want {\"x\":1}", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d never received the published payload", i)
		}
	}
}
