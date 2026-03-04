package handlers

import (
	"sync"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

var (
	queueHandlerInstance *QueueHandler
	queueHandlerOnce     sync.Once
)

type QueueHandler struct {
	queueService *services.QueueService
	authService  *services.AuthService
}

func NewQueueHandler(queueSvc *services.QueueService, authSvc *services.AuthService) *QueueHandler {
	queueHandlerOnce.Do(func() {
		queueHandlerInstance = &QueueHandler{
			queueService: queueSvc,
			authService:  authSvc,
		}
	})

	return queueHandlerInstance
}

type createQueueRequest struct {
	Lat            float64 `json:"lat" validate:"required"`
	Lng            float64 `json:"lng" validate:"required"`
	RadiusM        int     `json:"radius_m"`
	AvgServiceMins int     `json:"avg_service_mins"`
}

// Create creates a new queue. If no Authorization header, creates anonymously.
func (h *QueueHandler) Create(c fiber.Ctx) error {
	var req createQueueRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	hostID, _ := c.Locals("host_id").(string)
	hostPublicID := ""
	if pid, ok := c.Locals("host_public_id").(string); ok {
		hostPublicID = pid
	}

	var hostIDPtr, hostPublicIDPtr *string
	if hostID != "" {
		hostIDPtr = &hostID
	}
	if hostPublicID != "" {
		hostPublicIDPtr = &hostPublicID
	}

	queue, err := h.queueService.CreateQueue(c.Context(), services.CreateQueueParams{
		HostID:         hostIDPtr,
		HostPublicID:   hostPublicIDPtr,
		HostLat:        req.Lat,
		HostLng:        req.Lng,
		RadiusM:        req.RadiusM,
		AvgServiceMins: req.AvgServiceMins,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	response := fiber.Map{
		"queue_id":   queue.ID,
		"join_code":  queue.JoinCode,
		"status":     queue.Status,
		"expires_at": queue.ExpiresAt.Format(time.RFC3339),
	}

	// If anonymous (no auth), issue owner token
	if hostID == "" {
		ownerToken, err := h.authService.IssueAnonymousToken(c.Context(), queue.ID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError)
		}

		// Set as httpOnly cookie
		c.Cookie(&fiber.Cookie{
			Name:     "owner_token",
			Value:    ownerToken,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
			HTTPOnly: true,
			Secure:   true,
			SameSite: "Strict",
			Path:     "/",
		})

		response["owner_token"] = ownerToken
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

// GetStatus returns the public status of a queue.
func (h *QueueHandler) GetStatus(c fiber.Ctx) error {
	queueID := c.Params("id")

	queue, err := h.queueService.GetQueue(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}

	entries, _ := h.queueService.GetQueueEntries(c.Context(), queueID)
	waitingCount := 0
	for _, e := range entries {
		if e.Status == constants.EntryStatusWaiting {
			waitingCount++
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":               queue.ID,
		"status":           queue.Status,
		"join_code":        queue.JoinCode,
		"waiting_count":    waitingCount,
		"avg_service_mins": queue.AvgServiceMins,
		"expires_at":       queue.ExpiresAt.Format(time.RFC3339),
	})
}

type joinByCodeRequest struct {
	JoinCode    string  `json:"join_code" validate:"required,len=6"`
	Lat         float64 `json:"lat" validate:"required"`
	Lng         float64 `json:"lng" validate:"required"`
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

// JoinByCode resolves a join code and joins the queue.
func (h *QueueHandler) JoinByCode(c fiber.Ctx) error {
	var req joinByCodeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	// Resolve join code
	joinCodeSvc := services.NewJoinCodeService(nil) // Will use the singleton
	queueID, err := joinCodeSvc.ResolveJoinCode(c.Context(), req.JoinCode)
	if err != nil || queueID == "" {
		return fiber.NewError(fiber.StatusNotFound, "Invalid or expired queue code")
	}

	return h.joinQueue(c, queueID, req.Lat, req.Lng, req.FCMToken, req.DisplayName, req.PIN)
}

type joinRequest struct {
	Lat         float64 `json:"lat" validate:"required"`
	Lng         float64 `json:"lng" validate:"required"`
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

// JoinByID joins a queue directly by queue ID (QR scan path).
func (h *QueueHandler) JoinByID(c fiber.Ctx) error {
	queueID := c.Params("id")

	var req joinRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	return h.joinQueue(c, queueID, req.Lat, req.Lng, req.FCMToken, req.DisplayName, req.PIN)
}

func (h *QueueHandler) joinQueue(c fiber.Ctx, queueID string, lat, lng float64, fcmToken string, displayName, pin *string) error {
	result, err := h.queueService.JoinQueue(c.Context(), services.JoinQueueParams{
		QueueID:     queueID,
		Lat:         lat,
		Lng:         lng,
		FCMToken:    fcmToken,
		DisplayName: displayName,
		PIN:         pin,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// Heartbeat processes the 45s activity ping.
func (h *QueueHandler) Heartbeat(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	redisSvc := services.NewRedisService(nil) // Will use singleton
	if err := redisSvc.SetHeartbeat(c.Context(), queueID, token, time.Duration(constants.HeartbeatTTLSec)*time.Second); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

// PingUser buzzes a specific user.
func (h *QueueHandler) PingUser(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Params("token")

	if err := h.queueService.UpdateEntryStatus(c.Context(), queueID, token, constants.EntryStatusCalled); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// TODO: Send FCM push notification
	// TODO: Set idle timer in Redis

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "User pinged",
	})
}

// CallNext calls the next user in the queue.
func (h *QueueHandler) CallNext(c fiber.Ctx) error {
	queueID := c.Params("id")

	entry, err := h.queueService.CallNextUser(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	// TODO: Send FCM push notification
	// TODO: Set idle timer
	// TODO: Broadcast via WebSocket

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"token":     entry.Token,
		"ticket_no": entry.TicketNo,
		"status":    entry.Status,
	})
}

// RemoveUser removes a user from the queue.
func (h *QueueHandler) RemoveUser(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Params("token")

	if err := h.queueService.RemoveUser(c.Context(), queueID, token); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// TODO: Broadcast via WebSocket

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "User removed",
	})
}

// Close closes the queue.
func (h *QueueHandler) Close(c fiber.Ctx) error {
	queueID := c.Params("id")

	if err := h.queueService.CloseQueue(c.Context(), queueID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// TODO: FCM push to host and all users
	// TODO: Disconnect WS clients
	// TODO: Cleanup Redis keys

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Queue closed",
	})
}
