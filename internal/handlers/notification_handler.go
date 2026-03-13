package handlers

import (
	"sync"

	"queuebuzz/internal/requests"
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

// BroadcastToQueue godoc
// @Summary Broadcast notification to queue
// @Description Sends a push notification to all WAITING users in a specified queue
// @Tags Notification
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param request body requests.BroadcastRequest true "Broadcast Request payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/broadcast [post]
// @Security BearerAuth
func (h *NotificationHandler) BroadcastToQueue(c fiber.Ctx) error {
	queueID := c.Params("id")

	var req requests.BroadcastRequest
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
