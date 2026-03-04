package ws

import (
	"encoding/json"
	"sync"
	"time"

	"queuebuzz/internal/log"

	"github.com/fasthttp/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 4096 // 4KB
)

// Client represents a single WebSocket connection (host dashboard).
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	queueID string
	send    chan []byte
	once    sync.Once
}

// NewClient creates a Client bound to a queue.
func NewClient(hub *Hub, conn *websocket.Conn, queueID string) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		queueID: queueID,
		send:    make(chan []byte, 256),
	}
}

// ReadPump reads messages from the WebSocket connection.
// It handles client-sent events like visibility changes.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse incoming message (visibility changes etc.)
		var incoming Message
		if err := json.Unmarshal(msg, &incoming); err != nil {
			continue
		}

		switch incoming.Event {
		case EventVisibility:
			log.Debug().
				Str("queue_id", c.queueID).
				Str("event", EventVisibility).
				Msg("Host visibility changed")
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close cleanly shuts down the client.
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.send)
	})
}
