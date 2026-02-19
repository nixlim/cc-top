package state

import (
	"testing"
)

func TestMemoryStore_Close_ReturnsNil(t *testing.T) {
	store := NewMemoryStore()

	err := store.Close()
	if err != nil {
		t.Errorf("expected Close() to return nil for MemoryStore, got %v", err)
	}
}

func TestMemoryStore_OnEvent_Interface(t *testing.T) {
	var store Store = NewMemoryStore()

	called := false
	store.OnEvent(func(sessionID string, e Event) {
		called = true
	})

	e := Event{Name: "test.event"}
	store.AddEvent("sess-001", e)

	if !called {
		t.Error("expected OnEvent listener to be called")
	}
}

func TestMemoryStore_DroppedWrites_ReturnsZero(t *testing.T) {
	store := NewMemoryStore()

	dropped := store.DroppedWrites()
	if dropped != 0 {
		t.Errorf("expected DroppedWrites() to return 0 for MemoryStore, got %d", dropped)
	}
}

func TestMemoryStore_QueryDailySummaries_ReturnsEmpty(t *testing.T) {
	store := NewMemoryStore()

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 0 {
		t.Errorf("expected QueryDailySummaries() to return empty slice for MemoryStore, got %d items", len(summaries))
	}
}
