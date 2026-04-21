package http

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	"queuebuzz/internal/log"
	internalredis "queuebuzz/internal/redis"

	authservice "queuebuzz/internal/modules/auth/service"

	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/events"
	"queuebuzz/internal/modules/queue/jobs"
	"queuebuzz/internal/modules/queue/repository"

	queueservice "queuebuzz/internal/modules/queue/service"
	"queuebuzz/internal/sse"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Handler struct {
	cfg             *config.Config
	queueService    *queueservice.Service
	authService     *authservice.AuthService
	redisRepo       *repository.RedisRepository
	broker          *sse.Broker
	notifier        *queueservice.QueueNotifier
	posJob          *jobs.PositionJob
	hostNotifierJob *jobs.HostNotifierJob
	caller          *jobs.Caller
}

func NewHandler(
	cfg *config.Config,
	queueSvc *queueservice.Service,
	authSvc *authservice.AuthService,
	redisRepo *repository.RedisRepository,
	broker *sse.Broker,
	notifier *queueservice.QueueNotifier,
	posJob *jobs.PositionJob,
	hostNotifierJob *jobs.HostNotifierJob,
	caller *jobs.Caller,
) *Handler {
	return &Handler{
		cfg:             cfg,
		queueService:    queueSvc,
		authService:     authSvc,
		redisRepo:       redisRepo,
		broker:          broker,
		notifier:        notifier,
		posJob:          posJob,
		hostNotifierJob: hostNotifierJob,
		caller:          caller,
	}
}

// Create godoc
// @Summary Create a new queue
// @Description Creates a new queue for BOTH authenticated and anonymous host.
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "Queue created"
// @Param request body dto.CreateQueueRequest true "Create queue request"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.QueueRecord}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/create [post]
func (h *Handler) Create(c fiber.Ctx) error {
	var req dto.CreateQueueRequest
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

	isUserSlug := helpers.DerefString(req.Slug) != ""
	slug := strings.ToLower(helpers.DerefString(req.Slug))
	if slug == "" {
		slug = helpers.GenerateSlug()
	}

	if req.AllowPartyJoining == nil {
		defaultVal := false
		req.AllowPartyJoining = &defaultVal
	}

	if req.MaxPartySize == nil && *req.AllowPartyJoining {
		defaultVal := 10
		req.MaxPartySize = &defaultVal
	}

	var queue *domain.Queue
	var err error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		queue, err = h.queueService.CreateQueue(c.Context(), queueservice.CreateQueueParams{
			HostID:            hostIDPtr,
			HostPublicID:      hostPublicIDPtr,
			Name:              req.Name,
			Slug:              slug,
			AvgServiceMins:    *req.AvgServiceMins,
			AllowPartyJoining: req.AllowPartyJoining,
			MaxPartySize:      req.MaxPartySize,
			RecoveryEmail:     req.RecoveryEmail,
		})

		if err == nil {
			break
		}

		// Handle duplicate key/collision (E11000)
		if strings.Contains(err.Error(), "E11000") || strings.Contains(err.Error(), "duplicate") {
			if isUserSlug {
				return fiber.NewError(fiber.StatusConflict, "This custom slug is already taken. Please choose another one.")
			}

			// Our auto-generated slug collided, log and retry
			log.Warn().Str("slug", slug).Int("attempt", i+1).Msg("Slug collision detected, retrying with new slug")
			slug = helpers.GenerateSlug()
			continue
		}

		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create queue: "+err.Error())
	}

	response := dto.ToQueueResponse(*queue)

	if hostID == "" {
		token, err := h.authService.IssueAnonymousToken(c.Context(), queue.ID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}

		c.Cookie(&fiber.Cookie{
			Name:     "queuebuzz_host_token",
			Value:    token,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
			HTTPOnly: true,
			Secure:   h.cfg.IsProduction(),
			SameSite: "Lax",
			Path:     "/",
		})
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
// @Success 302 {object} helpers.SuccessResponse{Data=dto.QueueRecord}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/live [get]
func (h *Handler) GetLiveQueue(c fiber.Ctx) error {
	hostPublicID, _ := c.Locals("host_public_id").(string)
	queueID, _ := c.Locals("queue_id").(string)

	var queue *domain.Queue
	var err error

	if hostPublicID != "" {
		queue, err = h.queueService.GetLiveQueueForHost(c.Context(), hostPublicID)
	} else if queueID != "" {
		queue, err = h.queueService.GetLiveQueueByID(c.Context(), queueID)
	} else {
		return fiber.NewError(fiber.StatusUnauthorized, "no active host session found")
	}

	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	return helpers.NewSuccessResponse("Live queue fetched", dto.ToQueueResponse(*queue)).OK(c)
}

// GetLiveQueueByID godoc
// @Summary Get live queue by ID
// @Description Gets the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Success 200 {object} helpers.SuccessResponse{Data=dto.QueueRecord}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/live [get]
func (h *Handler) GetLiveQueueByID(c fiber.Ctx) error {
	hostPublicID, _ := c.Locals("host_public_id").(string)
	queue, err := h.queueService.GetLiveQueueByID(c.Context(), c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	// Security Check: If it's an authenticated host, it MUST be their own queue.
	// (Anonymous hosts use GetLiveQueue/Session, or don't have a broad public ID to check against here).
	if hostPublicID != "" && queue.HostPublicID != nil && *queue.HostPublicID != hostPublicID {
		return fiber.NewError(fiber.StatusForbidden, "unauthorized access to this queue")
	}

	return helpers.NewSuccessResponse("Live queue fetched", dto.ToQueueResponse(*queue)).OK(c)
}

// Events godoc
// @Summary Get live queue events
// @Description Gets the live queue events for the authenticated host.
// @Tags Queue
// @Produce text/event-stream
// @Success 200 {object} sse.Message "Stream of live queue events"
// @Header 200 {string} Connection "keep-alive"
// @Header 200 {string} Content-Type "text/event-stream"
// @Header 200 {string} Cache-Control "no-cache"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/events [get]
func (h *Handler) StreamEvents(c fiber.Ctx) error {
	queueID := c.Params("id")
	if queueID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "queue id is required")
	}

	snapshotFn := func() ([][]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		queue, err := h.queueService.GetQueue(ctx, queueID)
		if err != nil {
			return nil, err
		}
		entries, err := h.queueService.GetQueueEntries(ctx, queueID,
			constants.EntryStatusWaiting,
			constants.EntryStatusCalled,
			constants.EntryStatusIdle,
			constants.EntryStatusArrived,
			constants.EntryStatusServed,
		)
		if err != nil {
			return nil, err
		}

		var results [][]byte

		entriesData, _ := json.Marshal(events.Wrap(sse.NewMessage(events.EventQueueUpdate, dto.ToEntryResponses(entries))))
		results = append(results, entriesData)

		if queue.Status != constants.QueueStatusActive {
			statusData, _ := json.Marshal(events.Wrap(sse.NewMessage(events.EventQueueStatusChanged, events.QueueStatusData{Status: queue.Status})))
			results = append(results, statusData)
		}

		return results, nil
	}

	return h.broker.ServeHTTP(c, queueID, snapshotFn)
}

// PublicEvents godoc
// @Summary Get public queue events
// @Description Gets the public queue events for the authenticated host.
// @Tags Queue
// @Produce text/event-stream
// @Success 200 {object} sse.Message "Stream of public queue events"
// @Header 200 {string} Connection "keep-alive"
// @Header 200 {string} Content-Type "text/event-stream"
// @Header 200 {string} Cache-Control "no-cache"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/events/public [get]
func (h *Handler) PublicEvents(c fiber.Ctx) error {
	queueID := c.Params("id")
	if queueID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "queue id is required")
	}

	snapshotFn := func() ([][]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Initial wait count
		count, _ := h.queueService.GetWaitingCount(ctx, queueID)
		countMsg := events.Wrap(sse.NewMessage(events.EventWaitingCountUpdated, map[string]interface{}{"count": count}))
		countPayload, _ := json.Marshal(countMsg)

		return [][]byte{countPayload}, nil
	}

	return h.broker.ServeHTTP(c, "queue_public:"+queueID, snapshotFn)
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

	h.hostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusPaused)

	return helpers.NewSuccessResponse("Queue paused successfully", nil).OK(c)
}

// Update godoc
// @Summary Update queue settings
// @Description Updates the queue settings for the host (name, avg service mins, recovery email).
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param request body dto.UpdateQueueRequest true "Update queue request"
// @Success 200 {object} helpers.SuccessResponse{Data=nil} "Queue updated"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id} [patch]
func (h *Handler) Update(c fiber.Ctx) error {
	queueID := c.Params("id")
	var req dto.UpdateQueueRequest
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

	if req.StrictQueueMode != nil {
		updates["strict_queue_mode"] = *req.StrictQueueMode
	}

	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}

	if len(updates) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No fields to update")
	}

	queue, err := h.queueService.UpdateQueue(c.Context(), queueID, updates)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Queue updated", dto.ToQueueResponse(*queue)).OK(c)
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

	h.hostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusActive)

	return helpers.NewSuccessResponse("Queue resumed successfully", nil).OK(c)
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

	h.hostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusClosed)

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
// @Param name body dto.AddEntryRequest true "Add entry request"
// @Success 200 {object} map[string]interface{} "Entry added"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/add-entry [post]
func (h *Handler) AddEntry(c fiber.Ctx) error {
	var req dto.AddEntryRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	queueID := c.Params("id")
	createdBy := c.Locals("host_id").(string)
	if createdBy == "" {
		// Fallback to queue_id for anonymous hosts so it's not empty
		createdBy = c.Locals("queue_id").(string)
	}

	entry := domain.Entry{
		QueueID:   queueID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		PartySize: req.PartySize,
	}

	if createdBy != "" {
		entry.CreatedBy = &createdBy
	}

	result, err := h.queueService.CreateEntry(c.Context(), entry)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	entryRecord := dto.ToEntryResponse(result.Entry, result.Position)
	h.hostNotifierJob.DispatchUserJoined(result.Entry.QueueID, entryRecord)
	h.posJob.Dispatch(result.Entry.QueueID)

	return helpers.NewSuccessResponse("Entry added", entryRecord).OK(c)
}

// CallEntry godoc
// @Summary Call a new entry to the queue
// @Description Calls a new entry to the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param entry_id path string false "Entry ID"
// @Success 200 {object} map[string]interface{} "Entry called"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/call/{entry_id} [post]
func (h *Handler) CallEntry(c fiber.Ctx) error {
	queueID := c.Params("id")
	entryID := c.Params("entry_id")

	var entry *domain.Entry
	var err error

	if entryID == "" {
		// Acquire Lock to prevent race conditions during "Call Next"
		lockKey := internalredis.ActionLockKey(queueID, "call_next")
		ok, err := h.redisRepo.AcquireLock(c.Context(), lockKey, 3*time.Second)
		if err != nil || !ok {
			return fiber.NewError(fiber.StatusTooManyRequests, "Action in progress. Please wait.")
		}
		defer h.redisRepo.ReleaseLock(c.Context(), lockKey)

		// Business Logic: Strict Mode Violation Check
		queue, err := h.queueService.GetQueue(c.Context(), queueID)
		if err == nil && queue.StrictQueueMode {
			hasActive, _ := h.queueService.HasCalledEntries(c.Context(), queueID)
			if hasActive {
				return fiber.NewError(fiber.StatusConflict, "Please serve the current guest before calling the next one.")
			}
		}

		// Case: Call Next Guest
		entry, err = h.queueService.CallNextUser(c.Context(), queueID)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "No guests waiting in queue")
		}
		h.posJob.Dispatch(queueID)
	} else {
		// Case: Ping/Recall Specific Guest
		if err := h.queueService.UpdateEntryStatus(c.Context(), entryID, constants.EntryStatusCalled); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "Failed to call guest")
		}
		entry, err = h.queueService.GetEntry(c.Context(), entryID)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "Guest record not found")
		}
	}

	h.caller.DispatchCall(queueID, entry.ID, constants.EntryStatusCalled)
	h.hostNotifierJob.DispatchUserStatus(queueID, entry.ID, constants.EntryStatusCalled)

	return helpers.NewSuccessResponse("Guest called successfully", fiber.Map{
		"id":           entry.ID,
		"name":         entry.Name,
		"ticketNumber": entry.TicketNo,
		"status":       constants.EntryStatusCalled,
	}).OK(c)
}

// ServeEntry godoc
// @Summary Serve a new entry to the queue
// @Description Serves a new entry to the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param entry_id path string false "Entry ID"
// @Success 200 {object} map[string]interface{} "Entry served"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/serve/{entry_id} [post]
func (h *Handler) Serve(c fiber.Ctx) error {
	queueID := c.Params("id")
	entryID := c.Params("entry_id")

	if err := h.queueService.ServeUser(c.Context(), entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	h.hostNotifierJob.DispatchUserStatus(queueID, entryID, constants.EntryStatusServed)
	h.posJob.Dispatch(queueID)

	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) GetHistory(ctx fiber.Ctx) error {
	queueID := ctx.Params("id")
	response, err := h.queueService.GetHistoryDetail(ctx.Context(), queueID)
	if err != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return helpers.NewSuccessResponse("History detail fetched", response).OK(ctx)
}

func (h *Handler) GetHistoryList(c fiber.Ctx) error {
	hostPublicID, _ := c.Locals("host_public_id").(string)
	if hostPublicID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "host session not found")
	}

	search := c.Query("search")
	status := c.Query("filter")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	queues, total, err := h.queueService.GetQueueHistoryListForHost(c.Context(), hostPublicID, search, status, page, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	summary, _ := h.queueService.GetHostHistorySummary(c.Context(), hostPublicID)

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	response := dto.HistoryListResponse{
		Data:       make([]dto.QueueHistoryListItem, len(queues)),
		TotalCount: int(total),
		TotalPages: totalPages,
		Summary: dto.HistorySummary{
			TotalSessions:    0,
			TotalServed:      0,
			AvgSessionLength: "0m",
		},
	}

	if summary != nil {
		response.Summary.TotalSessions = summary.TotalSessions
		response.Summary.TotalServed = summary.TotalServed
		if summary.TotalServed > 0 {
			avgWait := time.Duration(summary.TotalWaitMS/int64(summary.TotalServed)) * time.Millisecond
			mins := int(avgWait.Minutes())
			if mins > 0 {
				response.Summary.AvgSessionLength = fmt.Sprintf("%dm", mins)
			} else {
				response.Summary.AvgSessionLength = fmt.Sprintf("%ds", int(avgWait.Seconds()))
			}
		}
	}

	for i, q := range queues {
		avgWaitStr := "0m"
		if q.AvgWaitMS > 0 {
			duration := time.Duration(q.AvgWaitMS) * time.Millisecond
			mins := int(duration.Minutes())
			if mins > 0 {
				avgWaitStr = fmt.Sprintf("%dm", mins)
			} else {
				avgWaitStr = fmt.Sprintf("%ds", int(duration.Seconds()))
			}
		}

		response.Data[i] = dto.QueueHistoryListItem{
			ID:            q.ID,
			Date:          q.CreatedAt.Format("2006-01-02"),
			DateFormatted: q.CreatedAt.Format("02 Jan, 2006"),
			Name:          q.Name,
			Status:        q.Status,
			TotalServed:   q.TotalServed,
			AvgWait:       avgWaitStr,
		}
	}

	return helpers.NewSuccessResponse("History list fetched", response).OK(c)
}

// RegisterHostFCM godoc
// @Summary Register host FCM token
// @Description Registers the host's FCM token for push notifications.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param token body string true "FCM Token"
// @Success 200 {object} map[string]interface{} "FCM token registered"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/register-fcm [post]
func (h *Handler) RegisterHostFCM(c fiber.Ctx) error {
	queueID := c.Params("id")
	var req struct {
		FcmToken string `json:"fcm_token"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	token := req.FcmToken
	if token == "" {
		return fiber.NewError(fiber.StatusBadRequest, "fcm token is required")
	}

	if err := h.queueService.RegisterHostFCM(c.Context(), queueID, token); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Host FCM token registered", nil).OK(c)
}

// UnregisterHostFCM godoc
// @Summary Unregister host FCM token
// @Description Unregisters the host's FCM token for push notifications.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "FCM token unregistered"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/unregister-fcm [post]
func (h *Handler) UnregisterHostFCM(c fiber.Ctx) error {
	queueID := c.Params("id")

	if err := h.queueService.UnregisterHostFCM(c.Context(), queueID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Host FCM token unregistered", nil).OK(c)
}
