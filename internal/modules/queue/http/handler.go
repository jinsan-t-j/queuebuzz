package http

import (
	"time"

	"queuebuzz/internal/constants"
	authservice "queuebuzz/internal/modules/auth/service"
	queuedto "queuebuzz/internal/modules/queue/dto"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	queueService    *queueservice.Service
	authService     *authservice.AuthService
	joinCodeService *legacyservices.JoinCodeService
	redisService    *legacyservices.RedisService
}

func NewHandler(queueSvc *queueservice.Service, authSvc *authservice.AuthService, joinCodeSvc *legacyservices.JoinCodeService, redisSvc *legacyservices.RedisService) *Handler {
	return &Handler{queueService: queueSvc, authService: authSvc, joinCodeService: joinCodeSvc, redisService: redisSvc}
}

func (h *Handler) Create(c fiber.Ctx) error {
	var req queuedto.CreateQueueRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	hostID, _ := c.Locals("host_id").(string)
	hostPublicID, _ := c.Locals("host_public_id").(string)
	var hostIDPtr, hostPublicIDPtr *string
	if hostID != "" {
		hostIDPtr = &hostID
	}

	if hostPublicID != "" {
		hostPublicIDPtr = &hostPublicID
	}

	queue, err := h.queueService.CreateQueue(c.Context(), queueservice.CreateQueueParams{
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

	if hostID == "" {
		anonHostToken, err := h.authService.IssueAnonymousToken(c.Context(), queue.ID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError)
		}
		c.Cookie(&fiber.Cookie{Name: "anon_host_token", Value: anonHostToken, Expires: time.Now().Add(30 * 24 * time.Hour), HTTPOnly: true, Secure: true, SameSite: "Strict", Path: "/"})
		response["anon_host_token"] = anonHostToken
	}
	return c.Status(fiber.StatusCreated).JSON(response)
}

func (h *Handler) GetStatus(c fiber.Ctx) error {
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

func (h *Handler) JoinByCode(c fiber.Ctx) error {
	var req queuedto.JoinByCodeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	queueID, err := h.joinCodeService.ResolveJoinCode(c.Context(), req.JoinCode)
	if err != nil || queueID == "" {
		return fiber.NewError(fiber.StatusNotFound, "Invalid or expired queue code")
	}
	return h.joinQueue(c, queueID, req.Lat, req.Lng, req.FCMToken, req.DisplayName, req.PIN)
}

func (h *Handler) JoinByID(c fiber.Ctx) error {
	var req queuedto.JoinRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	return h.joinQueue(c, c.Params("id"), req.Lat, req.Lng, req.FCMToken, req.DisplayName, req.PIN)
}

func (h *Handler) joinQueue(c fiber.Ctx, queueID string, lat, lng float64, fcmToken string, displayName, pin *string) error {
	result, err := h.queueService.JoinQueue(c.Context(), queueservice.JoinQueueParams{
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

func (h *Handler) Heartbeat(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")
	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}
	if err := h.redisService.SetHeartbeat(c.Context(), queueID, token, time.Duration(constants.HeartbeatTTLSec)*time.Second); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) PingUser(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Params("token")
	if err := h.queueService.UpdateEntryStatus(c.Context(), queueID, token, constants.EntryStatusCalled); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "User pinged"})
}

func (h *Handler) CallNext(c fiber.Ctx) error {
	entry, err := h.queueService.CallNextUser(c.Context(), c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"token":     entry.Token,
		"ticket_no": entry.TicketNo,
		"status":    entry.Status,
	})
}

func (h *Handler) RemoveUser(c fiber.Ctx) error {
	if err := h.queueService.RemoveUser(c.Context(), c.Params("id"), c.Params("token")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "User removed"})
}

func (h *Handler) Close(c fiber.Ctx) error {
	if err := h.queueService.CloseQueue(c.Context(), c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Queue closed"})
}
