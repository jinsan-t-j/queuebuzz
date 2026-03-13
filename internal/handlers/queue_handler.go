package handlers

import (
	"sync"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/requests"
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

// Create godoc
// @Summary Create a new queue
// @Description Creates a new queue. If no Authorization header is present, it creates an anonymous queue and returns an owner token.
// @Tags Queue
// @Accept json
// @Produce json
// @Param request body requests.CreateQueueRequest true "Create Queue Request payload"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues [post]
func (h *QueueHandler) Create(c fiber.Ctx) error {
	var req requests.CreateQueueRequest
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

// GetStatus godoc
// @Summary Get Queue Status
// @Description Returns the public status of a queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string "Error response"
// @Router /queues/{id}/status [get]
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

// JoinByCode godoc
// @Summary Join a queue via short-code
// @Description Resolves a 6-character join code and adds the user to the queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param request body requests.JoinByCodeRequest true "Join By Code Request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 404 {object} map[string]string "Error response"
// @Router /queues/join [post]
func (h *QueueHandler) JoinByCode(c fiber.Ctx) error {
	var req requests.JoinByCodeRequest
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

// JoinByID godoc
// @Summary Join a queue by ID
// @Description Joins a queue directly by its queue ID (often used via QR scanning)
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param request body requests.JoinRequest true "Join Request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Router /queues/{id}/join [post]
func (h *QueueHandler) JoinByID(c fiber.Ctx) error {
	queueID := c.Params("id")

	var req requests.JoinRequest
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

// Heartbeat godoc
// @Summary User Heartbeat
// @Description Processes the 45s activity ping from a user to keep their position active
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param X-User-Token header string true "User JWT session token"
// @Success 200 {string} string "OK"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/heartbeat [post]
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

// PingUser godoc
// @Summary Ping/buzz a user
// @Description Host specifically buzzes a user in the queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param token path string true "User session token"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/ping/{token} [post]
// @Security BearerAuth
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

// CallNext godoc
// @Summary Call next user
// @Description Host calls the next waiting user in the queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string "Error response"
// @Router /queues/{id}/call [post]
// @Security BearerAuth
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

// RemoveUser godoc
// @Summary Remove a user from queue
// @Description Host forcibly removes a specific user from the queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param token path string true "User session token"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/remove/{token} [post]
// @Security BearerAuth
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

// Close godoc
// @Summary Close a queue
// @Description Host closes a queue, disconnecting users and cleaning up
// @Tags Queue
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/close [post]
// @Security BearerAuth
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
