package sse

import (
	"sync"
)

// coalescingEvents lists SSE event types where only the latest value matters.
// Under pressure, stale intermediate events are overwritten instead of queued.
var coalescingEvents = map[string]bool{
	"position_update":       true,
	"waiting_count_updated": true,
	"heads_up":              true,
}

// IsCoalescing reports whether the given event type uses latest-wins semantics.
func IsCoalescing(eventType string) bool {
	return coalescingEvents[eventType]
}

// Subscriber is a smart per-client SSE delivery channel.
//
// It separates events into two paths:
//   - FIFO channel: ordered events (user_joined, user_called, etc.)
//   - Coalescing map: latest-wins events (position_update, waiting_count_updated)
//
// Under resource pressure, position events overwrite stale ones instead of
// being silently dropped. The SSE writer drains the coalescing map first,
// ensuring the client always receives the most recent position.
type Subscriber struct {
	fifo   chan []byte       // ordered events
	latest map[string][]byte // event type → latest pre-built SSE frame
	mu     sync.Mutex        // protects latest
	signal chan struct{}     // non-blocking signal that latest was updated
	closed bool
}

// NewSubscriber creates a subscriber with the given FIFO buffer capacity.
func NewSubscriber(bufSize int) *Subscriber {
	return &Subscriber{
		fifo:   make(chan []byte, bufSize),
		latest: make(map[string][]byte, 2),
		signal: make(chan struct{}, 1),
	}
}

// Send dispatches a frame to the subscriber.
//
// For coalescing events, the frame overwrites any previous unsent frame of the
// same event type — the client always gets the latest value.
// For ordered events, the frame is sent via the FIFO channel with drop on full.
func (s *Subscriber) Send(frame []byte, eventType string) {
	if IsCoalescing(eventType) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.latest[eventType] = frame
		s.mu.Unlock()

		// Non-blocking signal to wake the writer loop
		select {
		case s.signal <- struct{}{}:
		default:
		}
		return
	}

	// Ordered event — FIFO with drop on full
	select {
	case s.fifo <- frame:
	default:
	}
}

// DrainLatest returns all coalescing frames accumulated since the last drain
// and clears the map. The caller (SSE writer loop) should write these before
// processing the FIFO channel.
func (s *Subscriber) DrainLatest() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.latest) == 0 {
		return nil
	}

	frames := make([][]byte, 0, len(s.latest))
	for _, frame := range s.latest {
		frames = append(frames, frame)
	}
	// Reset the map to reclaim memory
	s.latest = make(map[string][]byte, 2)
	return frames
}

// FIFO returns the ordered event channel for select in the writer loop.
func (s *Subscriber) FIFO() <-chan []byte {
	return s.fifo
}

// Signal returns the coalescing notification channel for select in the writer loop.
func (s *Subscriber) Signal() <-chan struct{} {
	return s.signal
}

// Close marks the subscriber as closed and closes both channels.
// Safe to call multiple times.
func (s *Subscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true
	close(s.fifo)
	close(s.signal)
}
