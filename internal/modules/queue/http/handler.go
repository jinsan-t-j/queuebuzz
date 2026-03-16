package http

import (
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	authservice "queuebuzz/internal/modules/auth/service"
	"queuebuzz/internal/modules/queue/dto"
	queuedto "queuebuzz/internal/modules/queue/dto"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
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

// Create godoc
// @Summary Create a new queue
// @Description Creates a new queue for BOTH authenticated and anonymous host.
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "Queue created"
// @Param request body queuedto.CreateQueueRequest true "Create queue request"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.CreateQueueResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/create [post]
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

	if req.AvgServiceMins == 0 {
		req.AvgServiceMins = constants.DefaultAvgServiceMins
	}

	if req.Slug == "" {
		req.Slug = uuid.New().String()
	}

	queue, err := h.queueService.CreateQueue(c.Context(), queueservice.CreateQueueParams{
		HostID:         hostIDPtr,
		HostPublicID:   hostPublicIDPtr,
		Name:           req.QueueName,
		Slug:           req.Slug,
		AvgServiceMins: req.AvgServiceMins,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	response := dto.CreateQueueResponse{
		Name:      queue.Name,
		JoinCode:  queue.JoinCode,
		Slug:      queue.Slug,
		Status:    queue.Status,
		CreatedAt: queue.CreatedAt.Format(time.RFC3339),
		ExpiresAt: queue.ExpiresAt.Format(time.RFC3339),
	}

	if hostID == "" {
		anonHostToken, err := h.authService.IssueAnonymousToken(c.Context(), queue.ID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		c.Cookie(&fiber.Cookie{Name: "anon_host_token", Value: anonHostToken, Expires: time.Now().Add(30 * 24 * time.Hour), HTTPOnly: true, Secure: true, SameSite: "Strict", Path: "/"})
	}

	return helpers.NewSuccessResponse("Queue created", response).Created(c)
}

// CheckSlug godoc
// @Summary Check if a slug is available
// @Description Checks whether a queue slug is unique and available.
// @Tags Queue
// @Produce json
// @Param slug query string true "Slug to check"
// @Success 200 {object} helpers.SuccessResponse{Data=dto.CheckSlugResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/slug-check [get]
func (h *Handler) CheckSlug(c fiber.Ctx) error {
	slug := c.Query("slug")
	if slug == "" {
		return fiber.NewError(fiber.StatusBadRequest, "slug is required")
	}

	available, err := h.queueService.CheckSlugAvailability(c.Context(), slug)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to check slug availability")
	}

	return helpers.NewSuccessResponse("Slug availability checked", dto.CheckSlugResponse{IsAvailable: available}).OK(c)
}

// GetLiveQueue godoc
// @Summary Get live queue
// @Description Gets the live queue for the authenticated host.
// @Tags Host
// @Produce json
// @Success 200 {object} map[string]interface{} "Live queue"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.GetLiveQueueResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/:public_id/live [get]
func (h *Handler) GetLiveQueue(c fiber.Ctx) error {
	queue, err := h.queueService.GetLiveQueueForHost(c.Context(), c.Params("public_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}
	return helpers.NewSuccessResponse("Live queue fetched", queue).OK(c)
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
	return h.joinQueue(c, queueID, req.FCMToken, req.DisplayName, req.PIN)
}

func (h *Handler) JoinByID(c fiber.Ctx) error {
	var req queuedto.JoinRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	return h.joinQueue(c, c.Params("id"), req.FCMToken, req.DisplayName, req.PIN)
}

func (h *Handler) joinQueue(c fiber.Ctx, queueID string, fcmToken string, displayName, pin *string) error {
	result, err := h.queueService.JoinQueue(c.Context(), queueservice.JoinQueueParams{
		QueueID:     queueID,
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
