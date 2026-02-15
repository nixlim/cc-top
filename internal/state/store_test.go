package state

import (
	"sync"
	"testing"
	"time"
)

func TestStateStore_IndexMetricBySessionID(t *testing.T) {
	store := NewMemoryStore()

	m := Metric{
		Name:       "claude_code.cost.usage",
		Value:      1.50,
		Attributes: map[string]string{"model": "sonnet-4.5"},
		Timestamp:  time.Now(),
	}

	store.AddMetric("sess-001", m)

	// Verify metric is indexed under the correct session.
	s := store.GetSession("sess-001")
	if s == nil {
		t.Fatal("expected session 'sess-001' to exist")
	}
	if s.SessionID != "sess-001" {
		t.Errorf("expected SessionID='sess-001', got %q", s.SessionID)
	}
	if len(s.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(s.Metrics))
	}
	if s.Metrics[0].Name != "claude_code.cost.usage" {
		t.Errorf("expected metric name 'claude_code.cost.usage', got %q", s.Metrics[0].Name)
	}
	if s.TotalCost != 1.50 {
		t.Errorf("expected TotalCost=1.50, got %f", s.TotalCost)
	}

	// Verify another session does not have this metric.
	other := store.GetSession("sess-002")
	if other != nil {
		t.Error("expected session 'sess-002' to not exist")
	}
}

func TestStateStore_IndexEventBySessionID(t *testing.T) {
	store := NewMemoryStore()

	e := Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":    "sonnet-4.5",
			"cost_usd": "0.05",
		},
		Timestamp: time.Now(),
	}

	store.AddEvent("sess-abc", e)

	s := store.GetSession("sess-abc")
	if s == nil {
		t.Fatal("expected session 'sess-abc' to exist")
	}
	if len(s.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(s.Events))
	}
	if s.Events[0].Name != "claude_code.api_request" {
		t.Errorf("expected event name 'claude_code.api_request', got %q", s.Events[0].Name)
	}
	if s.Model != "sonnet-4.5" {
		t.Errorf("expected Model='sonnet-4.5', got %q", s.Model)
	}

	// Verify a different session does not have this event.
	other := store.GetSession("sess-def")
	if other != nil {
		t.Error("expected session 'sess-def' to not exist")
	}
}

func TestStateStore_EventDoesNotAccumulateCost(t *testing.T) {
	store := NewMemoryStore()

	// Add an api_request event with cost_usd attribute.
	e := Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":    "sonnet-4.5",
			"cost_usd": "0.05",
		},
		Timestamp: time.Now(),
	}
	store.AddEvent("sess-cost", e)

	s := store.GetSession("sess-cost")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	// Events must NOT accumulate cost — cost comes only from metrics.
	if s.TotalCost != 0.0 {
		t.Errorf("expected TotalCost=0.0 (events should not add cost), got %f", s.TotalCost)
	}
	// Model extraction should still work.
	if s.Model != "sonnet-4.5" {
		t.Errorf("expected Model='sonnet-4.5', got %q", s.Model)
	}
}

func TestStateStore_MissingSessID(t *testing.T) {
	store := NewMemoryStore()

	m := Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.50,
		Timestamp: time.Now(),
	}
	store.AddMetric("", m)

	// Metric should be stored under "unknown".
	s := store.GetSession(UnknownSessionID)
	if s == nil {
		t.Fatal("expected 'unknown' session to exist")
	}
	if len(s.Metrics) != 1 {
		t.Fatalf("expected 1 metric under 'unknown', got %d", len(s.Metrics))
	}

	// Also test events with empty session ID.
	e := Event{
		Name:      "claude_code.user_prompt",
		Timestamp: time.Now(),
	}
	store.AddEvent("", e)

	s = store.GetSession(UnknownSessionID)
	if s == nil {
		t.Fatal("expected 'unknown' session to exist after event")
	}
	if len(s.Events) != 1 {
		t.Fatalf("expected 1 event under 'unknown', got %d", len(s.Events))
	}
}

func TestStateStore_CounterReset(t *testing.T) {
	store := NewMemoryStore()

	// First metric value: 10.
	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     10.0,
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-001")
	if s.TotalCost != 10.0 {
		t.Errorf("expected TotalCost=10.0, got %f", s.TotalCost)
	}

	// Cumulative counter increases to 15 (delta = 5).
	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     15.0,
		Timestamp: time.Now(),
	})

	s = store.GetSession("sess-001")
	if s.TotalCost != 15.0 {
		t.Errorf("expected TotalCost=15.0, got %f", s.TotalCost)
	}

	// Counter reset: new value is 3 (less than previous 15).
	// Delta should be treated as 3 (previous treated as 0).
	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     3.0,
		Timestamp: time.Now(),
	})

	s = store.GetSession("sess-001")
	if s.TotalCost != 18.0 {
		t.Errorf("expected TotalCost=18.0 after counter reset, got %f", s.TotalCost)
	}
}

func TestStateStore_GetAggregatedCost(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     1.00,
		Timestamp: time.Now(),
	})
	store.AddMetric("sess-002", Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.50,
		Timestamp: time.Now(),
	})

	total := store.GetAggregatedCost()
	if total != 1.50 {
		t.Errorf("expected aggregated cost=1.50, got %f", total)
	}
}

func TestStateStore_UpdatePID(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})

	store.UpdatePID("sess-001", 4821)

	s := store.GetSession("sess-001")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	if s.PID != 4821 {
		t.Errorf("expected PID=4821, got %d", s.PID)
	}
}

func TestStateStore_MarkExited(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     2.50,
		Timestamp: time.Now(),
	})
	store.UpdatePID("sess-001", 4821)

	store.MarkExited(4821)

	s := store.GetSession("sess-001")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	if !s.Exited {
		t.Error("expected session to be marked exited")
	}
	if s.TotalCost != 2.50 {
		t.Errorf("expected cost preserved at 2.50, got %f", s.TotalCost)
	}
}

func TestStateStore_SessionCountSetsStartedAt(t *testing.T) {
	store := NewMemoryStore()

	metricTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	store.AddMetric("sess-ts", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: metricTime,
	})

	s := store.GetSession("sess-ts")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	// StartedAt should be the metric timestamp, not time.Now().
	if !s.StartedAt.Equal(metricTime) {
		t.Errorf("expected StartedAt=%v, got %v", metricTime, s.StartedAt)
	}
}

func TestStateStore_SessionCountDoesNotOverwriteStartedAt(t *testing.T) {
	store := NewMemoryStore()

	firstTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	laterTime := time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC)

	// First session.count creates the session.
	store.AddMetric("sess-ts2", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: firstTime,
	})

	// Second session.count should not overwrite StartedAt.
	store.AddMetric("sess-ts2", Metric{
		Name:      "claude_code.session.count",
		Value:     2,
		Timestamp: laterTime,
	})

	s := store.GetSession("sess-ts2")
	if !s.StartedAt.Equal(firstTime) {
		t.Errorf("expected StartedAt=%v (first metric), got %v", firstTime, s.StartedAt)
	}
}

func TestStateStore_SessionCountZeroTimestamp(t *testing.T) {
	store := NewMemoryStore()

	before := time.Now()
	store.AddMetric("sess-zero-ts", Metric{
		Name:  "claude_code.session.count",
		Value: 1,
		// Zero timestamp — should fallback to time.Now().
	})
	after := time.Now()

	s := store.GetSession("sess-zero-ts")
	if s.StartedAt.Before(before) || s.StartedAt.After(after) {
		t.Errorf("expected StartedAt near now, got %v (before=%v, after=%v)", s.StartedAt, before, after)
	}
}

func TestStateStore_MarkExited_IgnoresZeroPID(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})

	// Should not mark any session exited with PID 0.
	store.MarkExited(0)

	s := store.GetSession("sess-001")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	if s.Exited {
		t.Error("expected session NOT to be marked exited for PID=0")
	}
}

func TestStateStore_ListSessions(t *testing.T) {
	store := NewMemoryStore()

	// Add sessions in non-alphabetical order.
	store.AddMetric("sess-bravo", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})
	// Small delay to ensure ordering.
	time.Sleep(time.Millisecond)
	store.AddMetric("sess-alpha", Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})

	sessions := store.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// First session should be the one started first (bravo).
	if sessions[0].SessionID != "sess-bravo" {
		t.Errorf("expected first session='sess-bravo', got %q", sessions[0].SessionID)
	}
	if sessions[1].SessionID != "sess-alpha" {
		t.Errorf("expected second session='sess-alpha', got %q", sessions[1].SessionID)
	}
}

func TestStateStore_TokenMetric(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:       "claude_code.token.usage",
		Value:      2100,
		Attributes: map[string]string{"type": "input", "model": "sonnet-4.5"},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-001")
	if s.TotalTokens != 2100 {
		t.Errorf("expected TotalTokens=2100, got %d", s.TotalTokens)
	}
}

func TestStateStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sid := "sess-001"
			if n%2 == 0 {
				sid = "sess-002"
			}
			store.AddMetric(sid, Metric{
				Name:      "claude_code.cost.usage",
				Value:     float64(n),
				Timestamp: time.Now(),
			})
			store.AddEvent(sid, Event{
				Name:      "claude_code.user_prompt",
				Timestamp: time.Now(),
			})
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.GetSession("sess-001")
			store.ListSessions()
			store.GetAggregatedCost()
		}()
	}

	wg.Wait()

	sessions := store.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestStateStore_GetSessionReturnsNilForMissing(t *testing.T) {
	store := NewMemoryStore()
	s := store.GetSession("nonexistent")
	if s != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestStateStore_GetSessionReturnsCopy(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-001", Metric{
		Name:      "claude_code.cost.usage",
		Value:     1.0,
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-001")
	// Mutate the copy; original should be unaffected.
	s.TotalCost = 999.0
	s.Metrics = append(s.Metrics, Metric{Name: "injected"})

	original := store.GetSession("sess-001")
	if original.TotalCost == 999.0 {
		t.Error("mutation of copy should not affect store")
	}
	if len(original.Metrics) != 1 {
		t.Error("mutation of copy's metrics slice should not affect store")
	}
}

func TestStateStore_OrgAndUserFromEvent(t *testing.T) {
	store := NewMemoryStore()

	store.AddEvent("sess-id", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"organization.id":    "org-123",
			"user.account_uuid":  "user-abc",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-id")
	if s.OrgID != "org-123" {
		t.Errorf("expected OrgID='org-123', got %q", s.OrgID)
	}
	if s.UserUUID != "user-abc" {
		t.Errorf("expected UserUUID='user-abc', got %q", s.UserUUID)
	}
}

func TestStateStore_OrgAndUserFromMetric(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-id2", Metric{
		Name:  "claude_code.token.usage",
		Value: 100,
		Attributes: map[string]string{
			"organization.id":    "org-456",
			"user.account_uuid":  "user-def",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-id2")
	if s.OrgID != "org-456" {
		t.Errorf("expected OrgID='org-456', got %q", s.OrgID)
	}
	if s.UserUUID != "user-def" {
		t.Errorf("expected UserUUID='user-def', got %q", s.UserUUID)
	}
}

func TestStateStore_OrgAndUserAbsent(t *testing.T) {
	store := NewMemoryStore()

	store.AddEvent("sess-no-id", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"model": "sonnet-4.5"},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-no-id")
	if s.OrgID != "" {
		t.Errorf("expected OrgID='', got %q", s.OrgID)
	}
	if s.UserUUID != "" {
		t.Errorf("expected UserUUID='', got %q", s.UserUUID)
	}
}

func TestStateStore_OrgAndUserEmptyIgnored(t *testing.T) {
	store := NewMemoryStore()

	// Set initial values.
	store.AddEvent("sess-empty", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"organization.id":   "org-real",
			"user.account_uuid": "user-real",
		},
		Timestamp: time.Now(),
	})

	// Empty values should not overwrite.
	store.AddEvent("sess-empty", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"organization.id":   "",
			"user.account_uuid": "",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-empty")
	if s.OrgID != "org-real" {
		t.Errorf("expected OrgID='org-real' (not overwritten by empty), got %q", s.OrgID)
	}
	if s.UserUUID != "user-real" {
		t.Errorf("expected UserUUID='user-real' (not overwritten by empty), got %q", s.UserUUID)
	}
}

func TestStateStore_FastModeFromEvent(t *testing.T) {
	store := NewMemoryStore()

	// Event with speed attribute sets FastMode=true.
	store.AddEvent("sess-fast", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"speed": "fast"},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-fast")
	if !s.FastMode {
		t.Error("expected FastMode=true when speed attribute present")
	}

	// Event without speed attribute sets FastMode=false.
	store.AddEvent("sess-fast", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"model": "sonnet-4.5"},
		Timestamp:  time.Now(),
	})

	s = store.GetSession("sess-fast")
	if s.FastMode {
		t.Error("expected FastMode=false when speed attribute absent")
	}
}

func TestStateStore_FastModeFromMetric(t *testing.T) {
	store := NewMemoryStore()

	store.AddMetric("sess-fast-m", Metric{
		Name:       "claude_code.token.usage",
		Value:      100,
		Attributes: map[string]string{"speed": "fast"},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-fast-m")
	if !s.FastMode {
		t.Error("expected FastMode=true from metric with speed attribute")
	}
}

func TestStateStore_FastModeEmptySpeed(t *testing.T) {
	store := NewMemoryStore()

	// Empty speed attribute should NOT set FastMode.
	store.AddEvent("sess-empty-speed", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"speed": ""},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-empty-speed")
	if s.FastMode {
		t.Error("expected FastMode=false for empty speed attribute")
	}
}

func TestStateStore_EventSequenceOrdering(t *testing.T) {
	store := NewMemoryStore()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Add events out of sequence order.
	store.AddEvent("sess-seq", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"event.sequence": "3"},
		Timestamp:  base.Add(1 * time.Second),
	})
	store.AddEvent("sess-seq", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"event.sequence": "1"},
		Timestamp:  base.Add(2 * time.Second),
	})
	store.AddEvent("sess-seq", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"event.sequence": "2"},
		Timestamp:  base.Add(3 * time.Second),
	})

	s := store.GetSession("sess-seq")
	if len(s.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(s.Events))
	}
	// Events should be sorted by sequence: 1, 2, 3.
	if s.Events[0].Sequence != 1 {
		t.Errorf("expected first event sequence=1, got %d", s.Events[0].Sequence)
	}
	if s.Events[1].Sequence != 2 {
		t.Errorf("expected second event sequence=2, got %d", s.Events[1].Sequence)
	}
	if s.Events[2].Sequence != 3 {
		t.Errorf("expected third event sequence=3, got %d", s.Events[2].Sequence)
	}
}

func TestStateStore_EventSequenceBeforeNoSequence(t *testing.T) {
	store := NewMemoryStore()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Event without sequence (timestamp-based).
	store.AddEvent("sess-seq2", Event{
		Name:      "claude_code.user_prompt",
		Timestamp: base,
	})
	// Event with sequence should sort before event without.
	store.AddEvent("sess-seq2", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"event.sequence": "1"},
		Timestamp:  base.Add(1 * time.Second),
	})

	s := store.GetSession("sess-seq2")
	if len(s.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(s.Events))
	}
	// Event with sequence should come first.
	if s.Events[0].Sequence != 1 {
		t.Errorf("expected sequenced event first, got sequence=%d", s.Events[0].Sequence)
	}
	if s.Events[1].Sequence != 0 {
		t.Errorf("expected non-sequenced event second, got sequence=%d", s.Events[1].Sequence)
	}
}

func TestStateStore_EventTimestampFallback(t *testing.T) {
	store := NewMemoryStore()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Two events without sequences should sort by timestamp.
	store.AddEvent("sess-ts", Event{
		Name:      "claude_code.user_prompt",
		Timestamp: base.Add(2 * time.Second),
	})
	store.AddEvent("sess-ts", Event{
		Name:      "claude_code.user_prompt",
		Timestamp: base,
	})

	s := store.GetSession("sess-ts")
	if len(s.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(s.Events))
	}
	// Earlier timestamp should come first.
	if !s.Events[0].Timestamp.Before(s.Events[1].Timestamp) {
		t.Error("expected events sorted by timestamp (earlier first)")
	}
}

func TestStateStore_CacheTokenExtraction(t *testing.T) {
	store := NewMemoryStore()

	// Event with both cache token attributes.
	store.AddEvent("sess-cache", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":                 "sonnet-4.5",
			"cache_read_tokens":     "5000",
			"cache_creation_tokens": "2000",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-cache")
	if s == nil {
		t.Fatal("expected session to exist")
	}
	if s.CacheReadTokens != 5000 {
		t.Errorf("expected CacheReadTokens=5000, got %d", s.CacheReadTokens)
	}
	if s.CacheCreationTokens != 2000 {
		t.Errorf("expected CacheCreationTokens=2000, got %d", s.CacheCreationTokens)
	}
}

func TestStateStore_CacheTokenAccumulation(t *testing.T) {
	store := NewMemoryStore()

	// First event.
	store.AddEvent("sess-cache", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"cache_read_tokens":     "5000",
			"cache_creation_tokens": "2000",
		},
		Timestamp: time.Now(),
	})

	// Second event — values should accumulate, not replace.
	store.AddEvent("sess-cache", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"cache_read_tokens":     "3000",
			"cache_creation_tokens": "1000",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-cache")
	if s.CacheReadTokens != 8000 {
		t.Errorf("expected CacheReadTokens=8000, got %d", s.CacheReadTokens)
	}
	if s.CacheCreationTokens != 3000 {
		t.Errorf("expected CacheCreationTokens=3000, got %d", s.CacheCreationTokens)
	}
}

func TestStateStore_CacheTokenMissing(t *testing.T) {
	store := NewMemoryStore()

	// Event without cache token attributes.
	store.AddEvent("sess-nocache", Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"model": "sonnet-4.5"},
		Timestamp:  time.Now(),
	})

	s := store.GetSession("sess-nocache")
	if s.CacheReadTokens != 0 {
		t.Errorf("expected CacheReadTokens=0, got %d", s.CacheReadTokens)
	}
	if s.CacheCreationTokens != 0 {
		t.Errorf("expected CacheCreationTokens=0, got %d", s.CacheCreationTokens)
	}
}

func TestStateStore_CacheTokenMalformed(t *testing.T) {
	store := NewMemoryStore()

	// Malformed values should be ignored gracefully.
	store.AddEvent("sess-bad", Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"cache_read_tokens":     "not-a-number",
			"cache_creation_tokens": "",
		},
		Timestamp: time.Now(),
	})

	s := store.GetSession("sess-bad")
	if s.CacheReadTokens != 0 {
		t.Errorf("expected CacheReadTokens=0 for malformed value, got %d", s.CacheReadTokens)
	}
	if s.CacheCreationTokens != 0 {
		t.Errorf("expected CacheCreationTokens=0 for empty value, got %d", s.CacheCreationTokens)
	}
}

func TestSessionStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		session  SessionData
		expected SessionStatus
	}{
		{
			name:     "exited always returns StatusExited",
			session:  SessionData{Exited: true, LastEventAt: now},
			expected: StatusExited,
		},
		{
			name:     "active within 30s",
			session:  SessionData{LastEventAt: now.Add(-10 * time.Second)},
			expected: StatusActive,
		},
		{
			name:     "idle between 30s and 5m",
			session:  SessionData{LastEventAt: now.Add(-2 * time.Minute)},
			expected: StatusIdle,
		},
		{
			name:     "done after 5m",
			session:  SessionData{LastEventAt: now.Add(-10 * time.Minute)},
			expected: StatusDone,
		},
		{
			name:     "done with zero last event",
			session:  SessionData{},
			expected: StatusDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.Status()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestSessionHelpers(t *testing.T) {
	t.Run("TruncateSessionID", func(t *testing.T) {
		if got := TruncateSessionID("abcdefghij", 7); got != "abcd..." {
			t.Errorf("expected 'abcd...', got %q", got)
		}
		if got := TruncateSessionID("abc", 10); got != "abc" {
			t.Errorf("expected 'abc', got %q", got)
		}
		if got := TruncateSessionID("abc", 3); got != "abc" {
			t.Errorf("expected 'abc', got %q", got)
		}
		if got := TruncateSessionID("abcdef", 2); got != "ab" {
			t.Errorf("expected 'ab', got %q", got)
		}
	})

	t.Run("FilterSessionsByStatus", func(t *testing.T) {
		sessions := []SessionData{
			{SessionID: "s1", LastEventAt: time.Now()},                             // active
			{SessionID: "s2", LastEventAt: time.Now().Add(-2 * time.Minute)},       // idle
			{SessionID: "s3", LastEventAt: time.Now().Add(-10 * time.Minute)},      // done
			{SessionID: "s4", Exited: true, LastEventAt: time.Now()},               // exited
		}

		active := FilterSessionsByStatus(sessions, StatusActive)
		if len(active) != 1 || active[0].SessionID != "s1" {
			t.Errorf("expected 1 active session (s1), got %d", len(active))
		}
	})

	t.Run("ActiveSessions", func(t *testing.T) {
		sessions := []SessionData{
			{SessionID: "s1", Exited: false},
			{SessionID: "s2", Exited: true},
			{SessionID: "s3", Exited: false},
		}

		result := ActiveSessions(sessions)
		if len(result) != 2 {
			t.Errorf("expected 2 active sessions, got %d", len(result))
		}
	})

	t.Run("MetricsByName", func(t *testing.T) {
		s := &SessionData{
			Metrics: []Metric{
				{Name: "claude_code.cost.usage"},
				{Name: "claude_code.token.usage"},
				{Name: "claude_code.cost.usage"},
			},
		}
		result := MetricsByName(s, "claude_code.cost.usage")
		if len(result) != 2 {
			t.Errorf("expected 2 cost metrics, got %d", len(result))
		}
	})

	t.Run("EventsByName", func(t *testing.T) {
		s := &SessionData{
			Events: []Event{
				{Name: "claude_code.api_request"},
				{Name: "claude_code.api_error"},
				{Name: "claude_code.api_request"},
			},
		}
		result := EventsByName(s, "claude_code.api_request")
		if len(result) != 2 {
			t.Errorf("expected 2 api_request events, got %d", len(result))
		}
	})
}
