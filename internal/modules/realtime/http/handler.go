package http

import (
	"context"
	"time"

	"queuebuzz/internal/constants"
	authservice "queuebuzz/internal/modules/auth/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	"queuebuzz/internal/ws"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

type Handler struct {
	authService  *authservice.AuthService
	queueService *queueservice.Service
	hub          *ws.Hub
}

func NewHandler(authSvc *authservice.AuthService, queueSvc *queueservice.Service, hub *ws.Hub) *Handler {
	return &Handler{
		authService:  authSvc,
		queueService: queueSvc,
		hub:          hub,
	}
}

func (h *Handler) Handle(c fiber.Ctx) error {
	queueID := c.Params("id")
	tokenStr := c.Query("token")
	if tokenStr == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	claims, err := h.authService.VerifyToken(tokenStr)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

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

	upgrader := websocket.FastHTTPUpgrader{
		CheckOrigin: func(_ *fasthttp.RequestCtx) bool { return true },
	}

	fasthttpCtx := c.RequestCtx()
	return upgrader.Upgrade(fasthttpCtx, func(conn *websocket.Conn) {
		client := ws.NewClient(h.hub, conn, queueID)
		h.hub.Register(client)
		h.sendSnapshot(queueID)
		go client.WritePump()
		client.ReadPump()
	})
}

func (h *Handler) sendSnapshot(queueID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := h.queueService.GetQueueEntries(ctx, queueID)
	if err != nil {
		return
	}

	type safeEntry struct {
		Token       string  `json:"token"`
		TicketNo    string  `json:"ticket_no"`
		Position    int     `json:"position"`
		Status      string  `json:"status"`
		DisplayName *string `json:"display_name,omitempty"`
	}

	safe := make([]safeEntry, 0, len(entries))
	for _, entry := range entries {
		safe = append(safe, safeEntry{
			Token:       entry.Token,
			TicketNo:    entry.TicketNo,
			Position:    entry.Position,
			Status:      entry.Status,
			DisplayName: entry.DisplayName,
		})
	}

	h.hub.Broadcast(queueID, ws.Message{
		Event: ws.EventQueueUpdate,
		Data:  safe,
	})
}
