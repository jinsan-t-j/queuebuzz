package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"queuebuzz/internal/limit"
	"queuebuzz/internal/log"

	"github.com/gofiber/fiber/v3"
	redisdriver "github.com/redis/go-redis/v9"
)

const (
	// subscriberBufSize defines the per-subscriber FIFO channel capacity.
	subscriberBufSize = 16
	keepaliveInterval = 15 * time.Second
)

type envelope struct {
	Event string `json:"event"`
}

type BroadcastMessage struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

// Broker manages topic-based SSE subscriber channels.
type Broker struct {
	mu     sync.RWMutex
	subs   map[string][]*Subscriber
	pool   sync.Pool
	rdb    *redisdriver.Client
	closed bool
}

func NewBroker(rdb *redisdriver.Client) *Broker {
	b := &Broker{
		subs: make(map[string][]*Subscriber),
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
		rdb: rdb,
	}

	return b
}

// StartPubSub starts the Redis PubSub listener goroutine under lifecycle management.
// Call this from Container.Start() so the goroutine is tracked.
func (b *Broker) StartPubSub(ctx context.Context) {
	if b.rdb == nil {
		return
	}
	b.startPubSubListener(ctx)
}

func (b *Broker) Subscribe(topic string) (sub *Subscriber, unsubscribe func()) {
	bufSize := subscriberBufSize
	if limit.IsSystemOverloaded() {
		bufSize = 4
	}
	sub = NewSubscriber(bufSize)

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], sub)
	b.mu.Unlock()

	var once sync.Once
	unsubscribe = func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()

			subs := b.subs[topic]
			for i, current := range subs {
				if current != sub {
					continue
				}

				subs[i] = subs[len(subs)-1]
				subs[len(subs)-1] = nil
				subs = subs[:len(subs)-1]
				if len(subs) == 0 {
					delete(b.subs, topic)
				} else {
					b.subs[topic] = subs
				}
				sub.Close()
				return
			}
		})
	}

	return sub, unsubscribe
}

// Shutdown closes all active subscriber channels to force-terminate SSE streams.
func (b *Broker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for topic, subs := range b.subs {
		for _, sub := range subs {
			sub.Close()
		}
		delete(b.subs, topic)
	}
}

// Publish broadcasts an event payload to a topic, routed via Redis Pub/Sub if active.
func (b *Broker) Publish(topic string, event []byte) {
	if b.rdb != nil {
		msg, err := json.Marshal(BroadcastMessage{Topic: topic, Payload: event})
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			pubErr := b.rdb.Publish(ctx, "queuebuzz:sse:events", msg).Err()
			if pubErr == nil {
				return // Broadcast successfully delegated to Redis Pub/Sub
			}
			log.Warn().Err(pubErr).Msg("Failed to publish SSE event via Redis Pub/Sub; falling back to local dispatch")
		}
	}
	b.PublishLocal(topic, event)
}

// PublishLocal delivers the SSE frame directly to the locally connected subscribers.
// It detects the event type from the payload and routes coalescing events
// (position_update, waiting_count_updated) to the subscriber's latest-wins slot.
func (b *Broker) PublishLocal(topic string, event []byte) {
	// Extract event type for coalescing decision
	eventType := extractEventType(event)

	frame, ok := b.buildFrame(event)
	if !ok {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subs[topic] {
		sub.Send(frame, eventType)
	}
}

// startPubSubListener listens to Redis Pub/Sub and dispatches broadcasted events locally.
// It receives a context for lifecycle management and performs reconnection with backoff.
func (b *Broker) startPubSubListener(ctx context.Context) {
	go func() {
		backoff := time.Second
		maxBackoff := 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pubsub := b.rdb.Subscribe(ctx, "queuebuzz:sse:events")
			ch := pubsub.Channel()

			for msg := range ch {
				backoff = time.Second
				var bm BroadcastMessage
				if err := json.Unmarshal([]byte(msg.Payload), &bm); err == nil {
					b.PublishLocal(bm.Topic, bm.Payload)
				}
			}

			// Channel closed — connection lost. Reconnect with backoff.
			_ = pubsub.Close()

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			log.Warn().Dur("backoff", backoff).Msg("Redis PubSub connection lost, reconnecting...")

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()
}

func (b *Broker) Topics() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

func (b *Broker) buildFrame(payload []byte) ([]byte, bool) {
	if len(payload) == 0 {
		return nil, false
	}

	eventEnvelope := envelope{Event: "message"}
	_ = json.Unmarshal(payload, &eventEnvelope)

	buf := b.pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer b.pool.Put(buf)

	buf.WriteString("event:")
	buf.WriteString(eventEnvelope.Event)
	buf.WriteByte('\n')
	buf.WriteString("data:")
	_, _ = buf.Write(payload)
	buf.WriteString("\n\n")

	frame := make([]byte, buf.Len())
	copy(frame, buf.Bytes())
	return frame, true
}

// extractEventType reads the "event" field from a JSON payload without full unmarshal.
func extractEventType(payload []byte) string {
	var env envelope
	if err := json.Unmarshal(payload, &env); err == nil && env.Event != "" {
		return env.Event
	}
	return "message"
}

// ServeHTTP streams SSE events to a client with coalescing support.
//
// The writer loop has three select cases:
//  1. signal: drain all coalescing (latest-wins) frames first
//  2. fifo: ordered events in FIFO order
//  3. ticker: keepalive heartbeat
//  4. done: client disconnected
func (b *Broker) ServeHTTP(c fiber.Ctx, topic string, snapshot func() ([][]byte, error)) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	// Fiber strings (c.Params, c.Query) are reused after handler returns.
	// We must clone the topic because it's used as a map key in Subscribe
	// and accessed in the background by SendStreamWriter.
	topic = strings.Clone(topic)

	sub, unsub := b.Subscribe(topic)
	done := c.RequestCtx().Done()

	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer unsub()

		if snapshot != nil {
			payloads, err := snapshot()
			if err != nil {
				return
			}
			for _, payload := range payloads {
				if frame, ok := b.buildFrame(payload); ok {
					if _, err := w.Write(frame); err != nil {
						return
					}
				}
			}
			_ = w.Flush()
		}

		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()

		for {
			select {
			case <-sub.Signal():
				// Coalescing events ready — drain ALL latest-wins frames.
				// This ensures the client sees the most recent position,
				// even if multiple updates arrived while it was slow.
				frames := sub.DrainLatest()
				for _, frame := range frames {
					if _, err := w.Write(frame); err != nil {
						return
					}
				}
				if err := w.Flush(); err != nil {
					return
				}

			case msg, ok := <-sub.FIFO():
				if !ok {
					return
				}
				if _, err := w.Write(msg); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}

			case <-ticker.C:
				if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}

			case <-done:
				return
			}
		}
	})
}
