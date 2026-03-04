package handlers

import (
	"sync"

	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

var (
	notifHandlerInstance *NotificationHandler
	notifHandlerOnce     sync.Once
)

type NotificationHandler struct {
	queueService *services.QueueService
	notifSender  services.NotificationSender
}

func NewNotificationHandler(queueSvc *services.QueueService, notifSender services.NotificationSender) *NotificationHandler {
	notifHandlerOnce.Do(func() {
		notifHandlerInstance = &NotificationHandler{
			queueService: queueSvc,
			notifSender:  notifSender,
		}
	})

	return notifHandlerInstance
}

type broadcastRequest struct {
	Title string `json:"title" validate:"required,max=100"`
	Body  string `json:"body"  validate:"required,max=500"`
}

// BroadcastToQueue sends a notification to all WAITING users in a queue.
func (h *NotificationHandler) BroadcastToQueue(c fiber.Ctx) error {
	queueID := c.Params("id")

	var req broadcastRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	entries, err := h.queueService.GetQueueEntries(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	var tokens []string
	for _, e := range entries {
		if e.FCMToken != "" {
			tokens = append(tokens, e.FCMToken)
		}
	}

	if len(tokens) > 0 {
		_ = h.notifSender.SendToMultiple(c.Context(), tokens, req.Title, req.Body)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Broadcast sent",
		"count":   len(tokens),
	})
}
