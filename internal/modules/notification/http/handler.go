package http

import (
	"queuebuzz/internal/firebase"
	notificationdto "queuebuzz/internal/modules/notification/dto"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	queueService *queueservice.Service
	notifSender  firebase.NotificationSender
}

func NewHandler(queueSvc *queueservice.Service, notifSender firebase.NotificationSender) *Handler {
	return &Handler{
		queueService: queueSvc,
		notifSender:  notifSender,
	}
}

func (h *Handler) BroadcastToQueue(c fiber.Ctx) error {
	queueID := c.Params("id")

	var req notificationdto.BroadcastRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	entries, err := h.queueService.GetQueueEntries(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	var tokens []string
	for _, entry := range entries {
		if entry.FCMToken != nil && *entry.FCMToken != "" {
			tokens = append(tokens, *entry.FCMToken)
		}
	}

	if len(tokens) > 0 {
		_ = h.notifSender.SendToMultiple(c.Context(), tokens, req.Title, req.Body, nil)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Broadcast sent",
		"count":   len(tokens),
	})
}
