package ws

import (
	"encoding/json"
	"sync"

	"queuebuzz/internal/log"
)

// Hub maintains one active WebSocket client per queue_id.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // queue_id → *Client
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*Client),
	}
}

// Register adds a client for a queue. If a client already exists for the
// queue, it is replaced (old connection closed).
func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.clients[client.queueID]; ok {
		existing.Close()
	}

	h.clients[client.queueID] = client

	log.Info().
		Str("queue_id", client.queueID).
		Msg("WebSocket client registered")
}

// Unregister removes a client for a queue.
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.clients[client.queueID]; ok && existing == client {
		client.Close()
		delete(h.clients, client.queueID)

		log.Info().
			Str("queue_id", client.queueID).
			Msg("WebSocket client unregistered")
	}
}

// Broadcast sends a message to the host client for a specific queue.
func (h *Hub) Broadcast(queueID string, msg Message) {
	h.mu.RLock()
	client, ok := h.clients[queueID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to marshal WS message")
		return
	}

	select {
	case client.send <- data:
	default:
		// Client buffer full — disconnect
		h.Unregister(client)
	}
}

// DisconnectQueue forcibly disconnects the WebSocket client for a queue.
func (h *Hub) DisconnectQueue(queueID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[queueID]; ok {
		client.Close()
		delete(h.clients, queueID)

		log.Info().
			Str("queue_id", queueID).
			Msg("WebSocket client forcibly disconnected")
	}
}

// HasClient checks whether a queue has an active WebSocket connection.
func (h *Hub) HasClient(queueID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, ok := h.clients[queueID]
	return ok
}
