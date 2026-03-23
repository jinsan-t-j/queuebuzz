package http

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/exceptions"
	"queuebuzz/internal/helpers"
	authservice "queuebuzz/internal/modules/auth/service"
	"queuebuzz/internal/modules/queue/dto"
	queuedto "queuebuzz/internal/modules/queue/dto"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Handler struct {
	cfg             *config.Config
	queueService    *queueservice.Service
	authService     *authservice.AuthService
	joinCodeService *legacyservices.JoinCodeService
	redisService    *legacyservices.RedisService
	broker          *sse.Broker
	notifier        *queueservice.QueueNotifier
}

func NewHandler(
	cfg *config.Config,
	queueSvc *queueservice.Service,
	authSvc *authservice.AuthService,
	joinCodeSvc *legacyservices.JoinCodeService,
	redisSvc *legacyservices.RedisService,
	broker *sse.Broker,
	notifier *queueservice.QueueNotifier,
) *Handler {
	return &Handler{
		cfg:             cfg,
		queueService:    queueSvc,
		authService:     authSvc,
		joinCodeService: joinCodeSvc,
		redisService:    redisSvc,
		broker:          broker,
		notifier:        notifier,
	}
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

	if req.AvgServiceMins == nil {
		defaultMins := constants.DefaultAvgServiceMins
		req.AvgServiceMins = &defaultMins
	}

	if req.Slug == "" {
		req.Slug = uuid.New().String()
	}

	queue, err := h.queueService.CreateQueue(c.Context(), queueservice.CreateQueueParams{
		HostID:            hostIDPtr,
		HostPublicID:      hostPublicIDPtr,
		Name:              req.Name,
		Slug:              req.Slug,
		AvgServiceMins:    *req.AvgServiceMins,
		AllowPartyJoining: req.AllowPartyJoining,
		MaxPartySize:      req.MaxPartySize,
		RecoveryEmail:     req.RecoveryEmail,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	response := dto.CreateQueueResponse{
		QueueRecord: dto.QueueRecord{
			ID:                queue.ID,
			Name:              queue.Name,
			JoinCode:          queue.JoinCode,
			Slug:              queue.Slug,
			Status:            queue.Status,
			AvgServiceMins:    queue.AvgServiceMins,
			AllowPartyJoining: queue.AllowPartyJoining,
			MaxPartySize:      queue.MaxPartySize,
			CreatedAt:         queue.CreatedAt.Format(time.RFC3339),
			ExpiresAt:         queue.ExpiresAt.Format(time.RFC3339),
		},
	}

	if hostID == "" {
		anonHostToken, err := h.authService.IssueAnonymousToken(c.Context(), queue.ID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		c.Cookie(&fiber.Cookie{Name: "queuebuzz_host_token", Value: anonHostToken, Expires: time.Now().Add(30 * 24 * time.Hour), HTTPOnly: true, Secure: true, SameSite: "Strict", Path: "/"})
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
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "Live queue"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.GetLiveQueueResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/live [get]
func (h *Handler) GetLiveQueue(c fiber.Ctx) error {
	queue, err := h.queueService.GetLiveQueueForHost(c.Context(), c.Locals("host_public_id").(string))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	return helpers.NewSuccessResponse("Live queue fetched", dto.GetLiveQueueResponse{
		QueueRecord: dto.QueueRecord{
			ID:                queue.ID,
			Name:              queue.Name,
			JoinCode:          queue.JoinCode,
			Slug:              queue.Slug,
			Status:            queue.Status,
			AvgServiceMins:    queue.AvgServiceMins,
			AllowPartyJoining: queue.AllowPartyJoining,
			MaxPartySize:      queue.MaxPartySize,
			RecoveryEmail:     queue.RecoveryEmail,
			CreatedAt:         queue.CreatedAt.Format(time.RFC3339),
			ExpiresAt:         queue.ExpiresAt.Format(time.RFC3339),
		},
	}).OK(c)
}

// GetLiveQueueByID godoc
// @Summary Get live queue by ID
// @Description Gets the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Success 200 {object} helpers.SuccessResponse{Data=dto.GetLiveQueueResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/live [get]
func (h *Handler) GetLiveQueueByID(c fiber.Ctx) error {
	queue, err := h.queueService.GetLiveQueueByID(c.Context(), c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	return helpers.NewSuccessResponse("Live queue fetched", dto.GetLiveQueueResponse{
		QueueRecord: dto.QueueRecord{
			ID:                queue.ID,
			Name:              queue.Name,
			JoinCode:          queue.JoinCode,
			Slug:              queue.Slug,
			Status:            queue.Status,
			AvgServiceMins:    queue.AvgServiceMins,
			AllowPartyJoining: queue.AllowPartyJoining,
			MaxPartySize:      queue.MaxPartySize,
			RecoveryEmail:     queue.RecoveryEmail,
			CreatedAt:         queue.CreatedAt.Format(time.RFC3339),
			ExpiresAt:         queue.ExpiresAt.Format(time.RFC3339),
		},
	}).OK(c)
}

// Events godoc
// @Summary Get live queue events
// @Description Gets the live queue events for the authenticated host.
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "Live queue events"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.GetLiveQueueResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/events [get]
func (h *Handler) Events(c fiber.Ctx) error {
	queueID := c.Params("id")
	if queueID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "queue id is required")
	}

	snapshotFn := func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		entries, err := h.queueService.GetQueueEntries(ctx, queueID)
		if err != nil {
			return nil, err
		}

		return json.Marshal(queueservice.SSEMessage{
			Event: queueservice.EventQueueUpdate,
			Data:  dto.ToEntryResponses(entries),
		})
	}

	return h.broker.ServeHTTP(c, queueID, snapshotFn)
}

// PauseQueue godoc
// @Summary Pause a queue
// @Description Pauses the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "Queue paused"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/pause [post]
func (h *Handler) PauseQueue(c fiber.Ctx) error {
	queueID := c.Params("id")

	if err := h.queueService.PauseQueue(c.Context(), queueID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	h.notifier.PublishQueueStatus(queueID, constants.QueueStatusPaused)

	return helpers.NewSuccessResponse("Queue paused", nil).MessageResponse(c)
}

// Update godoc
// @Summary Update queue settings
// @Description Updates the queue settings for the host (name, avg service mins, recovery email).
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param request body queuedto.UpdateQueueRequest true "Update queue request"
// @Success 200 {object} helpers.SuccessResponse{Data=nil} "Queue updated"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id} [patch]
func (h *Handler) Update(c fiber.Ctx) error {
	queueID := c.Params("id")
	var req queuedto.UpdateQueueRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	updates := make(bson.M)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.AvgServiceMins != nil {
		updates["avg_service_mins"] = *req.AvgServiceMins
	}
	if req.RecoveryEmail != nil {
		updates["recovery_email"] = *req.RecoveryEmail
	}
	if req.Slug != nil {
		updates["slug"] = *req.Slug
	}
	if req.AllowPartyJoining != nil {
		updates["allow_party_joining"] = *req.AllowPartyJoining
	}
	if req.MaxPartySize != nil {
		updates["max_party_size"] = *req.MaxPartySize
	}

	if len(updates) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No fields to update")
	}

	if err := h.queueService.UpdateQueue(c.Context(), queueID, updates); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Queue updated", nil).OK(c)
}

// ResumeQueue godoc
// @Summary Resume a queue
// @Description Resumes the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "Queue resumed"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/resume [post]
func (h *Handler) ResumeQueue(c fiber.Ctx) error {
	queueID := c.Params("id")
	if err := h.queueService.ResumeQueue(c.Context(), queueID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	h.notifier.PublishQueueStatus(queueID, constants.QueueStatusActive)

	return helpers.NewSuccessResponse("Queue resumed", nil).MessageResponse(c)
}

// TerminateQueue godoc
// @Summary Close a queue
// @Description Closes the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "Queue closed"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/close [post]
func (h *Handler) TerminateQueue(c fiber.Ctx) error {
	queueID := c.Params("id")
	if err := h.queueService.TerminateQueue(c.Context(), queueID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Publish queue closed event
	h.notifier.PublishQueueExpired(queueID)

	c.Cookie(&fiber.Cookie{
		Name:     "queuebuzz_host_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})
	return c.SendStatus(fiber.StatusOK)
}

// AddEntry godoc
// @Summary Add a new entry to the queue
// @Description Adds a new entry to the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param name body queuedto.AddEntryRequest true "Add entry request"
// @Success 200 {object} map[string]interface{} "Entry added"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/add-entry [post]
func (h *Handler) AddEntry(c fiber.Ctx) error {
	var req queuedto.AddEntryRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	queueID := c.Params("id")
	createdBy := c.Locals("host_id").(string)
	result, err := h.queueService.JoinQueue(c.Context(), queueservice.JoinQueueParams{
		QueueID:   queueID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		PartySize: req.PartySize,
		CreatedBy: &createdBy,
	})
	if err != nil {
		var fieldEx *exceptions.FieldException
		if errors.As(err, &fieldEx) {
			return err
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	h.notifier.PublishEntryUpdate(result.Entry, int(result.Position))

	response := queuedto.AddEntryResponse{
		EntryRecord: queuedto.EntryRecord{
			ID:               result.ID,
			TicketNo:         result.TicketNo,
			Name:             result.Name,
			Status:           result.Status,
			EstimatedWaitMin: result.EstimatedWaitMin,
			PartySize:        result.PartySize,
		},
	}

	return helpers.NewSuccessResponse("Entry added", response).OK(c)
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
		QueueID:  queueID,
		FCMToken: &fcmToken,
		Name:     *displayName,
		PIN:      pin,
	})
	if err != nil {
		var fieldEx *exceptions.FieldException
		if errors.As(err, &fieldEx) {
			return err
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Publish queue update after user joins
	h.notifier.PublishEntryUpdate(result.Entry, int(result.Position))

	return c.Status(fiber.StatusOK).JSON(result)
}

func (h *Handler) Heartbeat(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")
	if token == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.redisService.SetHeartbeat(c.Context(), queueID, token, time.Duration(constants.HeartbeatTTLSec)*time.Second); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) PingUser(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Params("token")
	if err := h.queueService.UpdateEntryStatus(c.Context(), queueID, constants.EntryStatusCalled); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// Publish user called event
	h.notifier.PublishUserCalled(queueID, token, constants.EntryStatusCalled)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "User pinged"})
}

func (h *Handler) CallNext(c fiber.Ctx) error {
	queueID := c.Params("id")
	entry, err := h.queueService.CallNextUser(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	h.notifier.PublishUserCalled(queueID, entry.Token, constants.EntryStatusCalled)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"token":     entry.Token,
		"ticket_no": entry.TicketNo,
		"status":    entry.Status,
	})
}

func (h *Handler) GetHistory(ctx fiber.Ctx) error {
	queueSlug := ctx.Params("slug")
	entries, err := h.queueService.GetQueueHistory(ctx.Context(), queueSlug)
	if err != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	response := dto.QueueHistoryResponse{
		Entries: make([]dto.HistoryEntry, len(entries)),
	}

	for i, e := range entries {
		var waitTime int
		if e.FinishedAt != nil {
			waitTime = int(e.FinishedAt.Sub(e.JoinedAt).Minutes())
		} else {
			waitTime = int(time.Since(e.JoinedAt).Minutes())
		}

		var servedAt string
		if e.ServedAt != nil {
			servedAt = e.ServedAt.Format(time.RFC3339)
		}

		response.Entries[i] = dto.HistoryEntry{
			TicketNo:    e.TicketNo,
			DisplayName: helpers.DerefString(&e.Name),
			Status:      e.Status,
			WaitTimeMin: waitTime,
			ServedAt:    servedAt,
		}
	}

	return helpers.NewSuccessResponse("History fetched", response).OK(ctx)
}
