package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	subscriberBufSize = 8
	keepaliveInterval = 25 * time.Second
)

type envelope struct {
	Event string `json:"event"`
}

// Broker manages topic-based SSE subscriber channels.
type Broker struct {
	mu   sync.RWMutex
	subs map[string][]chan []byte
	pool sync.Pool
}

func NewBroker() *Broker {
	return &Broker{
		subs: make(map[string][]chan []byte),
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

func (b *Broker) Subscribe(topic string) (ch chan []byte, unsubscribe func()) {
	ch = make(chan []byte, subscriberBufSize)

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()

	var once sync.Once
	unsubscribe = func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()

			chs := b.subs[topic]
			for i, current := range chs {
				if current != ch {
					continue
				}

				chs[i] = chs[len(chs)-1]
				chs[len(chs)-1] = nil
				chs = chs[:len(chs)-1]
				if len(chs) == 0 {
					delete(b.subs, topic)
				} else {
					b.subs[topic] = chs
				}
				close(ch)
				return
			}
		})
	}

	return ch, unsubscribe
}

func (b *Broker) Publish(topic string, event []byte) {
	frame, ok := b.buildFrame(event)
	if !ok {
		return
	}

	fmt.Println("Publishing to topic:", topic)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs[topic] {
		select {
		case ch <- frame:
		default:
		}
	}
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

func (b *Broker) ServeHTTP(c fiber.Ctx, topic string, snapshot func() ([][]byte, error)) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	ch, unsub := b.Subscribe(topic)
	done := c.Context().Done()

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
			case msg, ok := <-ch:
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
