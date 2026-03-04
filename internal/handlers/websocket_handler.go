package handlers

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/services"
	"queuebuzz/internal/ws"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

var (
	wsHandlerInstance *WebSocketHandler
	wsHandlerOnce     sync.Once
)

type WebSocketHandler struct {
	authService  *services.AuthService
	queueService *services.QueueService
	hub          *ws.Hub
}

func NewWebSocketHandler(authSvc *services.AuthService, queueSvc *services.QueueService, hub *ws.Hub) *WebSocketHandler {
	wsHandlerOnce.Do(func() {
		wsHandlerInstance = &WebSocketHandler{
			authService:  authSvc,
			queueService: queueSvc,
			hub:          hub,
		}
	})

	return wsHandlerInstance
}

// Handle performs JWT validation, ownership verification, and WebSocket upgrade.
// Auth is validated BEFORE the handshake completes per the security checklist.
func (h *WebSocketHandler) Handle(c fiber.Ctx) error {
	queueID := c.Params("id")
	tokenStr := c.Query("token")

	if tokenStr == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	// Verify JWT before upgrading
	claims, err := h.authService.VerifyToken(tokenStr)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	// Only hosts can connect via WebSocket
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	switch claims.Role {
	case constants.RoleAnonymousHost:
		if claims.QueueID != queueID {
			return fiber.NewError(fiber.StatusForbidden)
		}
		if err := h.authService.VerifyAnonymousOwnership(ctx, tokenStr, queueID); err != nil {
			return fiber.NewError(fiber.StatusForbidden)
		}

	case constants.RoleRegisteredHost:
		queue, err := h.queueService.GetQueue(ctx, queueID)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound)
		}
		if queue.HostID == nil || *queue.HostID != claims.Subject {
			return fiber.NewError(fiber.StatusForbidden)
		}

	default:
		return fiber.NewError(fiber.StatusForbidden)
	}

	// All checks passed — upgrade to WebSocket using fasthttp
	upgrader := websocket.FastHTTPUpgrader{
		CheckOrigin: func(_ *fasthttp.RequestCtx) bool { return true },
	}

	// Get underlying fasthttp request context
	fasthttpCtx := c.RequestCtx()

	return upgrader.Upgrade(fasthttpCtx, func(conn *websocket.Conn) {
		client := ws.NewClient(h.hub, conn, queueID)
		h.hub.Register(client)

		// Send full queue snapshot on connect
		h.sendSnapshot(queueID)

		// Start pumps
		go client.WritePump()
		client.ReadPump() // Blocks until disconnect
	})
}

func (h *WebSocketHandler) sendSnapshot(queueID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := h.queueService.GetQueueEntries(ctx, queueID)
	if err != nil {
		return
	}

	// Filter out PII for the snapshot
	type safeEntry struct {
		Token       string  `json:"token"`
		TicketNo    string  `json:"ticket_no"`
		Position    int     `json:"position"`
		Status      string  `json:"status"`
		DisplayName *string `json:"display_name,omitempty"`
	}

	safe := make([]safeEntry, 0, len(entries))
	for _, e := range entries {
		safe = append(safe, safeEntry{
			Token:       e.Token,
			TicketNo:    e.TicketNo,
			Position:    e.Position,
			Status:      e.Status,
			DisplayName: e.DisplayName,
		})
	}

	h.hub.Broadcast(queueID, ws.Message{
		Event: ws.EventQueueUpdate,
		Data:  safe,
	})
}
