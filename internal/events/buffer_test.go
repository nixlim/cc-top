package events

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func makeEvent(session, eventType, formatted string) FormattedEvent {
	return FormattedEvent{
		SessionID: session,
		EventType: eventType,
		Formatted: formatted,
		Timestamp: time.Now(),
	}
}

func TestEventBuffer_Eviction(t *testing.T) {
	buf := NewRingBuffer(3)

	// Fill the buffer.
	buf.Add(makeEvent("s1", "api_request", "event-1"))
	buf.Add(makeEvent("s1", "api_request", "event-2"))
	buf.Add(makeEvent("s1", "api_request", "event-3"))

	if buf.Len() != 3 {
		t.Fatalf("expected len=3, got %d", buf.Len())
	}

	// Add one more; oldest (event-1) should be evicted.
	buf.Add(makeEvent("s1", "api_request", "event-4"))

	if buf.Len() != 3 {
		t.Fatalf("expected len=3 after eviction, got %d", buf.Len())
	}

	all := buf.ListAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}

	// Verify chronological order: event-2, event-3, event-4.
	expectedOrder := []string{"event-2", "event-3", "event-4"}
	for i, expected := range expectedOrder {
		if all[i].Formatted != expected {
			t.Errorf("position %d: expected %q, got %q", i, expected, all[i].Formatted)
		}
	}

	// Add two more; event-2 and event-3 should be evicted.
	buf.Add(makeEvent("s1", "api_request", "event-5"))
	buf.Add(makeEvent("s1", "api_request", "event-6"))

	all = buf.ListAll()
	expectedOrder = []string{"event-4", "event-5", "event-6"}
	for i, expected := range expectedOrder {
		if all[i].Formatted != expected {
			t.Errorf("position %d: expected %q, got %q", i, expected, all[i].Formatted)
		}
	}
}

func TestEventBuffer_CapacityOne(t *testing.T) {
	buf := NewRingBuffer(1)

	buf.Add(makeEvent("s1", "api_request", "first"))
	if buf.Len() != 1 {
		t.Fatalf("expected len=1, got %d", buf.Len())
	}

	all := buf.ListAll()
	if all[0].Formatted != "first" {
		t.Errorf("expected 'first', got %q", all[0].Formatted)
	}

	// Adding another evicts the first.
	buf.Add(makeEvent("s1", "api_request", "second"))
	if buf.Len() != 1 {
		t.Fatalf("expected len=1, got %d", buf.Len())
	}

	all = buf.ListAll()
	if all[0].Formatted != "second" {
		t.Errorf("expected 'second', got %q", all[0].Formatted)
	}
}

func TestEventBuffer_Empty(t *testing.T) {
	buf := NewRingBuffer(10)

	all := buf.ListAll()
	if all != nil {
		t.Errorf("expected nil for empty buffer, got %v", all)
	}
	if buf.Len() != 0 {
		t.Errorf("expected len=0, got %d", buf.Len())
	}
}

func TestEventBuffer_ListBySession(t *testing.T) {
	buf := NewRingBuffer(10)

	buf.Add(makeEvent("s1", "api_request", "s1-event-1"))
	buf.Add(makeEvent("s2", "api_request", "s2-event-1"))
	buf.Add(makeEvent("s1", "api_error", "s1-event-2"))
	buf.Add(makeEvent("s3", "user_prompt", "s3-event-1"))
	buf.Add(makeEvent("s2", "api_request", "s2-event-2"))

	s1Events := buf.ListBySession("s1")
	if len(s1Events) != 2 {
		t.Fatalf("expected 2 events for s1, got %d", len(s1Events))
	}
	if s1Events[0].Formatted != "s1-event-1" {
		t.Errorf("expected 's1-event-1', got %q", s1Events[0].Formatted)
	}
	if s1Events[1].Formatted != "s1-event-2" {
		t.Errorf("expected 's1-event-2', got %q", s1Events[1].Formatted)
	}

	s2Events := buf.ListBySession("s2")
	if len(s2Events) != 2 {
		t.Fatalf("expected 2 events for s2, got %d", len(s2Events))
	}

	// Non-existent session.
	s4Events := buf.ListBySession("s4")
	if len(s4Events) != 0 {
		t.Errorf("expected 0 events for s4, got %d", len(s4Events))
	}
}

func TestEventBuffer_ListByType(t *testing.T) {
	buf := NewRingBuffer(10)

	buf.Add(makeEvent("s1", "api_request", "req-1"))
	buf.Add(makeEvent("s1", "api_error", "err-1"))
	buf.Add(makeEvent("s2", "api_request", "req-2"))
	buf.Add(makeEvent("s2", "user_prompt", "prompt-1"))

	requests := buf.ListByType("api_request")
	if len(requests) != 2 {
		t.Fatalf("expected 2 api_request events, got %d", len(requests))
	}

	errors := buf.ListByType("api_error")
	if len(errors) != 1 {
		t.Fatalf("expected 1 api_error event, got %d", len(errors))
	}

	decisions := buf.ListByType("tool_decision")
	if len(decisions) != 0 {
		t.Errorf("expected 0 tool_decision events, got %d", len(decisions))
	}
}

func TestEventBuffer_PartialFill(t *testing.T) {
	buf := NewRingBuffer(5)

	buf.Add(makeEvent("s1", "api_request", "event-1"))
	buf.Add(makeEvent("s1", "api_request", "event-2"))

	if buf.Len() != 2 {
		t.Errorf("expected len=2, got %d", buf.Len())
	}
	if buf.Cap() != 5 {
		t.Errorf("expected cap=5, got %d", buf.Cap())
	}

	all := buf.ListAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}
	if all[0].Formatted != "event-1" || all[1].Formatted != "event-2" {
		t.Error("events not in expected order")
	}
}

func TestEventBuffer_WrapAround(t *testing.T) {
	buf := NewRingBuffer(3)

	// Fill and wrap around multiple times.
	for i := 0; i < 10; i++ {
		buf.Add(makeEvent("s1", "api_request", fmt.Sprintf("event-%d", i)))
	}

	all := buf.ListAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}

	// Should contain events 7, 8, 9.
	for i, expected := range []string{"event-7", "event-8", "event-9"} {
		if all[i].Formatted != expected {
			t.Errorf("position %d: expected %q, got %q", i, expected, all[i].Formatted)
		}
	}
}

func TestEventBuffer_ConcurrentAccess(t *testing.T) {
	buf := NewRingBuffer(100)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.Add(makeEvent(
				fmt.Sprintf("s%d", n%5),
				"api_request",
				fmt.Sprintf("event-%d", n),
			))
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf.ListAll()
			buf.ListBySession("s0")
			buf.ListByType("api_request")
			buf.Len()
		}()
	}

	wg.Wait()

	if buf.Len() != 50 {
		t.Errorf("expected len=50, got %d", buf.Len())
	}
}

func TestEventBuffer_LargeEviction(t *testing.T) {
	buf := NewRingBuffer(1000)

	// Add 1001 events; first should be evicted.
	for i := 0; i < 1001; i++ {
		buf.Add(makeEvent("s1", "api_request", fmt.Sprintf("event-%d", i)))
	}

	if buf.Len() != 1000 {
		t.Fatalf("expected len=1000, got %d", buf.Len())
	}

	all := buf.ListAll()
	// First event should be event-1 (event-0 was evicted).
	if all[0].Formatted != "event-1" {
		t.Errorf("expected first event to be 'event-1', got %q", all[0].Formatted)
	}
	// Last event should be event-1000.
	if all[999].Formatted != "event-1000" {
		t.Errorf("expected last event to be 'event-1000', got %q", all[999].Formatted)
	}
}

func TestEventBuffer_ZeroCapacity(t *testing.T) {
	// Zero capacity should be clamped to 1.
	buf := NewRingBuffer(0)
	if buf.Cap() != 1 {
		t.Errorf("expected cap=1 for zero capacity input, got %d", buf.Cap())
	}

	buf.Add(makeEvent("s1", "api_request", "test"))
	if buf.Len() != 1 {
		t.Errorf("expected len=1, got %d", buf.Len())
	}
}
