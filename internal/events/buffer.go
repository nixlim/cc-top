package events

import "sync"

// RingBuffer is a fixed-capacity, thread-safe ring buffer for FormattedEvents.
// When the buffer is full, the oldest event is evicted to make room for new entries.
// All methods are safe for concurrent use.
type RingBuffer struct {
	mu    sync.RWMutex
	items []FormattedEvent
	cap   int
	head  int // index of the oldest element
	count int // number of elements currently stored
}

// NewRingBuffer creates a new RingBuffer with the given capacity.
// Capacity must be at least 1. A buffer with capacity=1 holds exactly 1 event.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		items: make([]FormattedEvent, capacity),
		cap:   capacity,
	}
}

// Add inserts an event into the buffer. If the buffer is full, the oldest
// event is overwritten.
func (rb *RingBuffer) Add(e FormattedEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Calculate write position.
	writePos := (rb.head + rb.count) % rb.cap
	if rb.count == rb.cap {
		// Buffer is full; overwrite oldest and advance head.
		rb.items[rb.head] = e
		rb.head = (rb.head + 1) % rb.cap
	} else {
		rb.items[writePos] = e
		rb.count++
	}
}

// ListAll returns all events in chronological order (oldest first).
func (rb *RingBuffer) ListAll() []FormattedEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return rb.listLocked()
}

// ListBySession returns all events for the given session ID in
// chronological order.
func (rb *RingBuffer) ListBySession(sessionID string) []FormattedEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	all := rb.listLocked()
	var result []FormattedEvent
	for _, e := range all {
		if e.SessionID == sessionID {
			result = append(result, e)
		}
	}
	return result
}

// ListByType returns all events of the given type in chronological order.
// The type should match the EventType field (e.g., "user_prompt", "api_request").
func (rb *RingBuffer) ListByType(eventType string) []FormattedEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	all := rb.listLocked()
	var result []FormattedEvent
	for _, e := range all {
		if e.EventType == eventType {
			result = append(result, e)
		}
	}
	return result
}

// Len returns the number of events currently in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Cap returns the capacity of the buffer.
func (rb *RingBuffer) Cap() int {
	return rb.cap
}

// listLocked returns all events in chronological order.
// Caller must hold at least a read lock.
func (rb *RingBuffer) listLocked() []FormattedEvent {
	if rb.count == 0 {
		return nil
	}
	result := make([]FormattedEvent, rb.count)
	for i := 0; i < rb.count; i++ {
		result[i] = rb.items[(rb.head+i)%rb.cap]
	}
	return result
}
