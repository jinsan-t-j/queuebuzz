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

	billingservice "queuebuzz/internal/modules/billing/service"
	"queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Handler struct {
	cfg              *config.Config
	Service          *queueservice.Service
	AnalyticsService *queueservice.AnalyticsService
	AuthService      *authservice.AuthService
	HostService      *service.Service
	RedisRepo        *repository.RedisRepository
	Broker           *sse.Broker
	Notifier         *queueservice.QueueNotifier
	PosJob           *jobs.PositionJob
	HostNotifierJob  *jobs.HostNotifierJob
	Caller           *jobs.Caller
	BillingSvc       *billingservice.BillingService
	EmailService     *legacyservices.EmailService
}

func NewHandler(
	cfg *config.Config,
	queueSvc *queueservice.Service,
	analyticsSvc *queueservice.AnalyticsService,
	authSvc *authservice.AuthService,
	hostSvc *service.Service,
	redisRepo *repository.RedisRepository,
	broker *sse.Broker,
	notifier *queueservice.QueueNotifier,
	posJob *jobs.PositionJob,
	hostNotifierJob *jobs.HostNotifierJob,
	caller *jobs.Caller,
	billingSvc *billingservice.BillingService,
	emailSvc *legacyservices.EmailService,
) *Handler {
	return &Handler{
		cfg:              cfg,
		Service:          queueSvc,
		AnalyticsService: analyticsSvc,
		AuthService:      authSvc,
		HostService:      hostSvc,
		RedisRepo:        redisRepo,
		Broker:           broker,
		Notifier:         notifier,
		PosJob:           posJob,
		HostNotifierJob:  hostNotifierJob,
		Caller:           caller,
		BillingSvc:       billingSvc,
		EmailService:     emailSvc,
	}
}

func (h *Handler) toQueueResponse(ctx context.Context, queue domain.Queue) dto.QueueRecord {
	var profileImg, bannerImg string
	if queue.HostID != nil {
		if host, err := h.HostService.FindByID(ctx, *queue.HostID); err == nil && host != nil {
			profileImg = helpers.DerefString(host.ProfileImageURL)
			bannerImg = helpers.DerefString(host.BannerImageURL)
		}
	}
	return dto.ToQueueResponse(queue, profileImg, bannerImg)
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
	if err := c.Bind().Body(&req); err != nil {
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

	hasUserSlug := helpers.DerefString(req.Slug) != ""

	if hasUserSlug {
		role, _ := c.Locals("role").(string)
		if role != constants.RoleRegisteredHost {
			return fiber.NewError(fiber.StatusForbidden,
				"Custom queue URLs require a QueueBuzz account. Sign up free to unlock this feature.")
		}
	}

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

	if req.IsGeoLocked != nil && *req.IsGeoLocked {
		role, _ := c.Locals("role").(string)
		if role != constants.RoleRegisteredHost {
			return fiber.NewError(fiber.StatusForbidden, "Geo-Lockdown is a premium feature. Please sign up or sign in to upgrade.")
		}
		plan, err := h.BillingSvc.GetHostPlan(c.Context(), hostID)
		if err != nil || !plan.Limits.AllowGeoLock {
			return fiber.NewError(fiber.StatusForbidden, "Geo-Lockdown is a premium feature. Please upgrade your plan to unlock this feature.")
		}
		if req.Latitude == nil || req.Longitude == nil {
			return fiber.NewError(fiber.StatusBadRequest, "Coordinates (latitude and longitude) are required when enabling Geo-Lockdown.")
		}
	}

	var queue *domain.Queue
	var err error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		queue, err = h.Service.CreateQueue(c.Context(), queueservice.CreateQueueParams{
			HostID:            hostIDPtr,
			HostPublicID:      hostPublicIDPtr,
			Name:              req.Name,
			Slug:              slug,
			AvgServiceMins:    *req.AvgServiceMins,
			AllowPartyJoining: req.AllowPartyJoining,
			MaxPartySize:      req.MaxPartySize,
			ManualPositioning: req.ManualPositioning,
			IsGeoLocked:       req.IsGeoLocked,
			Latitude:          req.Latitude,
			Longitude:         req.Longitude,
			GeoRadiusMeters:   req.GeoRadiusMeters,
		})

		if err == nil {
			break
		}

		if strings.Contains(err.Error(), "invalid slug format") || strings.Contains(err.Error(), "slug is reserved") {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}

		// Handle duplicate key/collision (E11000)
		if strings.Contains(err.Error(), "E11000") || strings.Contains(err.Error(), "duplicate") {
			if hasUserSlug {
				return fiber.NewError(fiber.StatusConflict, "This custom slug is already taken. Please choose another one.")
			}

			// Our auto-generated slug collided, log and retry
			log.Warn().Str("slug", slug).Int("attempt", i+1).Msg("Slug collision detected, retrying with new slug")
			slug = helpers.GenerateSlug()
			continue
		}

		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create queue: "+err.Error())
	}

	response := h.toQueueResponse(c.Context(), *queue)

	if hostID == "" {
		token, err := h.AuthService.IssueAnonymousToken(c.Context(), queue.ID)
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

	if hostID != "" && h.RedisRepo != nil {
		_ = h.RedisRepo.InvalidateHostHistory(c.Context(), hostID)
		if hostPublicID != "" {
			_ = h.RedisRepo.InvalidateHistorySummary(c.Context(), hostPublicID)
		}
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

	available, err := h.Service.CheckSlugAvailability(c.Context(), slug)
	if err != nil {
		if strings.Contains(err.Error(), "invalid slug format") {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
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
		queue, err = h.Service.GetLiveQueueForHost(c.Context(), hostPublicID)
	} else if queueID != "" {
		queue, err = h.Service.GetLiveQueueByID(c.Context(), queueID)
	} else {
		return fiber.NewError(fiber.StatusUnauthorized, "no active host session found")
	}

	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	return helpers.NewSuccessResponse("Live queue fetched", h.toQueueResponse(c.Context(), *queue)).OK(c)
}

// FindActiveQueueByIDOrSlugOrCode godoc
// @Summary Find a queue by id/slug/code or join code
// @Description Finds the queue using an ID/slug/code or a 6-character code.
// @Tags Queue
// @Produce json
// @Param id query string false "Queue ID or Slug"
// @Param code query string false "Join Code"
// @Success 200 {object} helpers.SuccessResponse{Data=dto.QueueRecord} "Queue found"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 404 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/p/find [get]
func (h *Handler) FindActiveQueueByIDOrSlugOrCode(c fiber.Ctx) error {
	id := c.Query("id")
	code := c.Query("code")

	var queue *domain.Queue
	var err error

	if code != "" {
		queueID, err := h.RedisRepo.ResolveJoinCode(c.Context(), code)
		if err != nil || queueID == "" {
			// Fallback to database for join code
			queue, err = h.Service.GetActiveQueueByJoinCode(c.Context(), code)

			if queue == nil {
				if err != nil {
					return fiber.NewError(fiber.StatusNotFound, err.Error())
				}
				return fiber.NewError(fiber.StatusNotFound, "Invalid or expired queue code")
			}

			ttl := time.Until(queue.ExpiresAt) + (time.Duration(constants.JoinCodeTTLExtraH) * time.Hour)
			_ = h.RedisRepo.SetJoinCode(c.Context(), code, queue.ID, ttl)
		} else {
			queue, err = h.Service.GetQueue(c.Context(), queueID)
			if err != nil {
				return fiber.NewError(fiber.StatusNotFound, "Queue not found")
			}
		}

		if id != "" && queue.ID != id && queue.Slug != id {
			return fiber.NewError(fiber.StatusNotFound, "Invalid code for this queue")
		}
	} else if id != "" {
		queue, err = h.Service.GetLiveQueueByID(c.Context(), id)
		if err != nil || queue == nil {
			return fiber.NewError(fiber.StatusNotFound, "Queue not found")
		}
	} else {
		return fiber.NewError(fiber.StatusBadRequest, "must provide id or code")
	}

	return helpers.NewSuccessResponse("Live queue found", h.toQueueResponse(c.Context(), *queue)).OK(c)
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
	queue, err := h.Service.GetLiveQueueByID(c.Context(), c.Params("id"))
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

	return helpers.NewSuccessResponse("Live queue fetched", h.toQueueResponse(c.Context(), *queue)).OK(c)
}

// GetPublicStatus godoc
// @Summary Get public queue status
// @Description Gets public queue status along with active entries (masked).
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} helpers.SuccessResponse{Data=map[string]interface{}}
// @Failure 404 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/p/{id}/public-status [get]
func (h *Handler) GetPublicStatus(c fiber.Ctx) error {
	queueID := c.Params("id")
	queue, err := h.Service.GetLiveQueueByID(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if queue == nil {
		return fiber.NewError(fiber.StatusNotFound, "live queue not found")
	}

	entries, err := h.Service.GetQueueEntries(c.Context(), queue.ID,
		constants.EntryStatusWaiting,
		constants.EntryStatusCalled,
		constants.EntryStatusArrived,
		constants.EntryStatusIdle,
	)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	entryResponses := dto.ToEntryResponses(entries)
	for i := range entryResponses {
		entryResponses[i].Email = nil
		entryResponses[i].Phone = nil
		if queue.ManualPositioning {
			entryResponses[i].Position = 0
		}
	}

	return helpers.NewSuccessResponse("Public status fetched", fiber.Map{
		"queue":   h.toQueueResponse(c.Context(), *queue),
		"entries": entryResponses,
	}).OK(c)
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
	queueID := strings.Clone(c.Params("id"))
	if queueID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "queue id is required")
	}

	// Fail early if queue is expired/not found
	_, err := h.Service.GetQueue(c.Context(), queueID)
	if err != nil {
		if strings.Contains(err.Error(), "no documents in result") {
			return fiber.NewError(fiber.StatusGone, "Queue has ended or expired")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	hostID, _ := c.Locals("host_id").(string)
	snapshotFn := func() ([][]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		queue, err := h.Service.GetQueue(ctx, queueID)
		if err != nil {
			return nil, err
		}
		entries, err := h.Service.GetQueueEntries(ctx, queueID,
			constants.EntryStatusWaiting,
			constants.EntryStatusCalled,
			constants.EntryStatusIdle,
			constants.EntryStatusArrived,
			constants.EntryStatusServed,
			constants.EntryStatusLeft,
		)
		if err != nil {
			return nil, err
		}

		var results [][]byte
		maskedEntries := h.maskEntriesIfRequired(ctx, hostID, dto.ToEntryResponses(entries))

		entriesData, _ := json.Marshal(events.Wrap(sse.NewMessage(events.EventQueueUpdate, maskedEntries)))
		results = append(results, entriesData)

		if queue.Status != constants.QueueStatusActive {
			statusData, _ := json.Marshal(events.Wrap(sse.NewMessage(events.EventQueueStatusChanged, events.QueueStatusData{Status: queue.Status})))
			results = append(results, statusData)
		}

		return results, nil
	}

	return h.Broker.ServeHTTP(c, queueID, snapshotFn)
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
	queueID := strings.Clone(c.Params("id"))
	if queueID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "queue id is required")
	}

	// Fail early if queue is expired/not found
	queue, err := h.Service.GetLiveQueueByID(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if queue == nil {
		return fiber.NewError(fiber.StatusGone, "Queue has ended or expired")
	}
	queueID = queue.ID

	snapshotFn := func() ([][]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Initial wait count
		count, _ := h.Service.GetWaitingCount(ctx, queueID)
		countMsg := events.Wrap(sse.NewMessage(events.EventWaitingCountUpdated, map[string]interface{}{"count": count}))
		countPayload, _ := json.Marshal(countMsg)

		return [][]byte{countPayload}, nil
	}

	return h.Broker.ServeHTTP(c, "queue_public:"+queueID, snapshotFn)
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

	if err := h.Service.PauseQueue(c.Context(), queueID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	h.HostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusPaused)

	return helpers.NewSuccessResponse("Queue paused successfully", nil).OK(c)
}

// Update godoc
// @Summary Update queue settings
// @Description Updates the queue settings for the host (name, avg service mins).
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
	if req.BufferMins != nil {
		if *req.BufferMins > 0 {
			updates["delay_expires_at"] = time.Now().Add(time.Duration(*req.BufferMins) * time.Minute)
		} else {
			updates["delay_expires_at"] = time.Time{}
		}
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

	if req.IsGeoLocked != nil {
		role, _ := c.Locals("role").(string)
		if role != constants.RoleRegisteredHost {
			return fiber.NewError(fiber.StatusForbidden, "Geo-Lockdown is a premium feature. Please sign up or sign in to upgrade.")
		}
		hostID, _ := c.Locals("host_id").(string)
		plan, err := h.BillingSvc.GetHostPlan(c.Context(), hostID)
		if err != nil || !plan.Limits.AllowGeoLock {
			return fiber.NewError(fiber.StatusForbidden, "Geo-Lockdown is a premium feature. Please upgrade your plan to unlock this feature.")
		}
		updates["is_geo_locked"] = *req.IsGeoLocked
	}
	if req.Latitude != nil {
		updates["latitude"] = *req.Latitude
	}
	if req.Longitude != nil {
		updates["longitude"] = *req.Longitude
	}
	if req.GeoRadiusMeters != nil {
		updates["geo_radius_meters"] = *req.GeoRadiusMeters
	}

	if len(updates) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "No fields to update")
	}

	queue, err := h.Service.UpdateQueue(c.Context(), queueID, updates)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	h.PosJob.Dispatch(queueID)

	return helpers.NewSuccessResponse("Queue updated", h.toQueueResponse(c.Context(), *queue)).OK(c)
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
	if err := h.Service.ResumeQueue(c.Context(), queueID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	h.HostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusActive)

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
	hostID, _ := c.Locals("host_id").(string)
	hostPublicID, _ := c.Locals("host_public_id").(string)
	queueID := c.Params("id")

	// Get active queue details first (while active/paused) to use for notifications
	queue, _ := h.Service.GetQueue(c.Context(), queueID)

	unserved, err := h.Service.TerminateQueue(c.Context(), queueID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	h.HostNotifierJob.DispatchQueueStatus(queueID, constants.QueueStatusClosed)

	// Notify unserved guests
	if len(unserved) > 0 {
		queueName := "Your Queue"
		if queue != nil {
			queueName = queue.Name
		}

		for _, entry := range unserved {
			h.Notifier.NotifyQueueEnded(entry.ID, queueName)
		}
	}

	if h.RedisRepo != nil && hostID != "" {
		_ = h.RedisRepo.InvalidateHostHistory(c.Context(), hostID)
		if hostPublicID != "" {
			_ = h.RedisRepo.InvalidateHistorySummary(c.Context(), hostPublicID)
		}
	}

	if c.Cookies("access_token") == "" {
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
	}
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
	if err := c.Bind().Body(&req); err != nil {
		return err
	}

	if req.Email != nil && *req.Email != "" {
		if err := h.EmailService.ValidateEmail(*req.Email); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	queueID := c.Params("id")
	createdBy, _ := c.Locals("host_id").(string)
	if createdBy == "" {
		// Fallback to queue_id for anonymous hosts so it's not empty
		createdBy, _ = c.Locals("queue_id").(string)
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

	result, err := h.Service.CreateEntry(c.Context(), entry)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	entryRecord := dto.ToEntryResponse(result.Entry, result.Position)
	h.HostNotifierJob.DispatchUserJoined(result.QueueID, entryRecord)
	h.PosJob.Dispatch(result.QueueID)

	// Mask PII for the public response
	maskedRecord := entryRecord
	maskedRecord.Email = helpers.MaskEmail(entryRecord.Email)
	maskedRecord.Phone = helpers.MaskPhone(entryRecord.Phone)

	return helpers.NewSuccessResponse("Successfully joined the queue", maskedRecord).Created(c)
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
		lock := internalredis.NewLock(h.RedisRepo.Client(), lockKey)
		acquired, lockErr := lock.Acquire(c.Context(), 3*time.Second)
		if lockErr != nil || !acquired {
			return fiber.NewError(fiber.StatusTooManyRequests, "Action in progress. Please wait.")
		}
		defer func() { _ = lock.Release(c.Context()) }()

		// Business Logic: Strict Mode Violation Check
		queue, err := h.Service.GetQueue(c.Context(), queueID)
		if err == nil && queue.StrictQueueMode {
			hasActive, _ := h.Service.HasCalledEntries(c.Context(), queueID)
			if hasActive {
				return fiber.NewError(fiber.StatusConflict, "Please serve the current guest before calling the next one.")
			}
		}

		// Case: Call Next Guest
		entry, err = h.Service.CallNextUser(c.Context(), queueID)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "No guests waiting in queue")
		}
		_, _ = h.Service.UpdateQueue(c.Context(), queueID, bson.M{"delay_expires_at": time.Time{}})
		h.PosJob.Dispatch(queueID)

		// Heads-up: notify the next 2 waiting guests that they're almost up
		calledID := entry.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			ids, err := h.RedisRepo.GetQueueEntryIDsRange(ctx, queueID, 0, 4)
			if err != nil || len(ids) == 0 {
				return
			}
			var nextUp []string
			for _, id := range ids {
				if id == calledID {
					continue
				}
				e, err := h.Service.GetEntry(ctx, id)
				if err != nil || e.Status != constants.EntryStatusWaiting {
					continue
				}
				nextUp = append(nextUp, id)
				if len(nextUp) >= 2 {
					break
				}
			}
			if len(nextUp) > 0 {
				h.Notifier.NotifyHeadsUp(queueID, nextUp)
			}
		}()
	} else {
		// Case: Ping/Recall Specific Guest
		if err := h.Service.UpdateEntryStatus(c.Context(), entryID, constants.EntryStatusCalled); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "Failed to call guest")
		}
		entry, err = h.Service.GetEntry(c.Context(), entryID)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "Guest record not found")
		}
	}

	h.Caller.DispatchCall(queueID, entry.ID, constants.EntryStatusCalled)
	h.HostNotifierJob.DispatchUserStatus(queueID, entry.ID, constants.EntryStatusCalled)

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

	if err := h.Service.ServeUser(c.Context(), entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	h.HostNotifierJob.DispatchUserStatus(queueID, entryID, constants.EntryStatusServed)
	h.PosJob.Dispatch(queueID)

	return c.SendStatus(fiber.StatusOK)
}

// Skip godoc
// @Summary Skip an entry in the queue
// @Description Skips an entry in the live queue for the authenticated host.
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Param entry_id path string true "Entry ID"
// @Success 200 {object} helpers.SuccessResponse{Data=nil} "Entry skipped"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/manage/{id}/skip/{entry_id} [post]
func (h *Handler) Skip(c fiber.Ctx) error {
	queueID := c.Params("id")
	entryID := c.Params("entry_id")

	if err := h.Service.SkipUser(c.Context(), entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	h.HostNotifierJob.DispatchUserStatus(queueID, entryID, constants.EntryStatusSkipped)
	h.PosJob.Dispatch(queueID)

	return c.SendStatus(fiber.StatusOK)
}

// GetHistory godoc
// @Summary Get the queue history
// @Description Gets the history of a queue for the host
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "History fetched"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/manage/{id}/history [get]
func (h *Handler) GetHistory(ctx fiber.Ctx) error {
	queueID := ctx.Params("id")

	hostID, _ := ctx.Locals("host_id").(string)
	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "Missing host ID")
	}

	canAccess, _ := h.BillingSvc.CheckHistoryAccess(ctx.Context(), hostID)
	if !canAccess {
		return fiber.NewError(fiber.StatusForbidden, "History access not allowed on current plan")
	}
	if h.RedisRepo != nil && hostID != "" {
		if data, err := h.RedisRepo.GetHistoryDetail(ctx.Context(), hostID, queueID); err == nil {
			var cached dto.HistoryDetailResponse
			if json.Unmarshal(data, &cached) == nil {
				h.maskHistoryIfRequired(ctx.Context(), hostID, &cached)
				return helpers.NewSuccessResponse("History detail fetched (cached)", cached).OK(ctx)
			}
		}
	}

	response, err := h.Service.GetHistoryDetail(ctx.Context(), queueID)
	if err != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if h.RedisRepo != nil && hostID != "" {
		_ = h.RedisRepo.SetHistoryDetail(ctx.Context(), hostID, queueID, response)
	}

	h.maskHistoryIfRequired(ctx.Context(), hostID, response)

	return helpers.NewSuccessResponse("History detail fetched", response).OK(ctx)
}

// GetHistoryList godoc
// @Summary Get the queues history
// @Description Gets the history of all queues for the host.
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "History fetched"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/history [get]
func (h *Handler) GetHistoryList(c fiber.Ctx) error {
	hostPublicID, _ := c.Locals("host_public_id").(string)
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "Missing host ID")
	}

	search := c.Query("search")
	status := c.Query("filter")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	if h.RedisRepo != nil {
		if data, err := h.RedisRepo.GetHistoryList(c.Context(), hostID, search, status, page, limit); err == nil {
			var cached dto.HistoryListResponse
			if json.Unmarshal(data, &cached) == nil {
				return helpers.NewSuccessResponse("History list fetched (cached)", cached).OK(c)
			}
		}
	}

	queues, total, err := h.Service.GetQueueHistoryListForHost(c.Context(), hostID, hostPublicID, search, status, page, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	summary, _ := h.Service.GetHostHistorySummary(c.Context(), hostID, hostPublicID)

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

	if h.RedisRepo != nil {
		_ = h.RedisRepo.SetHistoryList(c.Context(), hostID, search, status, page, limit, response)
		// Also cache the summary for the host
		if summary != nil {
			_ = h.RedisRepo.SetHistorySummary(c.Context(), hostID, summary)
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

	if err := h.Service.RegisterHostFCM(c.Context(), queueID, token); err != nil {
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

	if err := h.Service.UnregisterHostFCM(c.Context(), queueID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Host FCM token unregistered", nil).OK(c)
}

// DeleteHistory godoc
// @Summary Delete queue history
// @Description Deletes a single queue's entire history (queue, entries, and related data).
// @Tags Queue
// @Produce json
// @Param id path string true "Queue ID"
// @Success 200 {object} map[string]interface{} "History deleted successfully"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/{id}/history [delete]
func (h *Handler) DeleteHistory(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	queueID := c.Params("id")

	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "host session not found")
	}

	if err := h.Service.DeleteQueue(c.Context(), hostID, queueID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete queue history")
	}

	if h.RedisRepo != nil {
		_ = h.RedisRepo.InvalidateHostHistory(c.Context(), hostID)
		_ = h.RedisRepo.InvalidateHistoryDetail(c.Context(), hostID, queueID)
		_ = h.RedisRepo.InvalidateHistorySummary(c.Context(), hostID)
	}

	return helpers.NewSuccessResponse("History deleted successfully", nil).OK(c)
}

// DeleteHistoryBulk godoc
// @Summary Delete multiple queue histories
// @Description Deletes multiple queue histories at once.
// @Tags Queue
// @Produce json
// @Param ids body []string true "Array of queue IDs"
// @Success 200 {object} map[string]interface{} "History deleted successfully"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/history/bulk [post]
func (h *Handler) DeleteHistoryBulk(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "host session not found")
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	if len(req.IDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no ids provided")
	}

	if err := h.Service.DeleteQueuesBulk(c.Context(), hostID, req.IDs); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete bulk history")
	}

	if h.RedisRepo != nil {
		_ = h.RedisRepo.InvalidateHostHistory(c.Context(), hostID)
		_ = h.RedisRepo.InvalidateHistorySummary(c.Context(), hostID)
		for _, id := range req.IDs {
			_ = h.RedisRepo.InvalidateHistoryDetail(c.Context(), hostID, id)
		}
	}

	return helpers.NewSuccessResponse("Selected history items deleted", nil).OK(c)
}

// ClearHistory godoc
// @Summary Clear all history
// @Description Clears all queue history for the host.
// @Tags Queue
// @Produce json
// @Success 200 {object} map[string]interface{} "History cleared successfully"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queue/history [delete]
func (h *Handler) ClearHistory(c fiber.Ctx) error {
	hostPublicID, _ := c.Locals("host_public_id").(string)
	if hostPublicID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "host session not found")
	}

	if err := h.Service.ClearHostHistory(c.Context(), hostPublicID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to clear history")
	}

	if h.RedisRepo != nil {
		hostID, _ := c.Locals("host_id").(string)
		if hostID != "" {
			_ = h.RedisRepo.InvalidateHostHistory(c.Context(), hostID)
			_ = h.RedisRepo.InvalidateHistorySummary(c.Context(), hostID)
		}
	}

	return helpers.NewSuccessResponse("History cleared successfully", nil).OK(c)
}

// GetDashboard godoc
// @Summary Get dashboard metrics
// @Description Gets real-time and historical metrics for the host dashboard.
// @Tags Dashboard
// @Produce json
// @Success 200 {object} helpers.SuccessResponse{Data=dto.DashboardData}
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /queue/dashboard [get]
func (h *Handler) GetDashboard(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	hostPublicID, _ := c.Locals("host_public_id").(string)

	if hostID == "" || hostPublicID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "host session not found")
	}

	hostName := "Host"
	totalQueues := 0
	totalServed := 0
	host, err := h.HostService.FindByID(c.Context(), hostID)
	if err == nil && host != nil {
		hostName = host.Name
		totalQueues = host.TotalQueueCount
		totalServed = host.TotalServedCount
	}

	data, err := h.AnalyticsService.GetDashboardData(c.Context(), hostPublicID, hostName)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load dashboard metrics: "+err.Error())
	}

	// Enrich dashboard data with persistent host stats
	data.HasHistory = totalQueues > 0
	if data.HasHistory {
		// Milestone 1: Create your first queue
		if len(data.QuickSetup.Steps) > 0 {
			data.QuickSetup.Steps[0].IsDone = true
		}
		// Milestone 2: Serve your first guest
		if totalServed > 0 && len(data.QuickSetup.Steps) > 1 {
			data.QuickSetup.Steps[1].IsDone = true
		}
		// Hide Quick Setup once they've created 5+ queues overall
		if totalQueues >= 5 {
			data.QuickSetup.Show = false
		}
	}

	return helpers.NewSuccessResponse("Dashboard data fetched", data).OK(c)
}

func (h *Handler) maskEntriesIfRequired(ctx context.Context, hostID string, entries []dto.EntryRecord) []dto.EntryRecord {
	canView, _ := h.BillingSvc.CanViewGuestData(ctx, hostID)
	if canView {
		return entries
	}

	for i := range entries {
		entries[i].Email = helpers.MaskEmail(entries[i].Email)
		entries[i].Phone = helpers.MaskPhone(entries[i].Phone)
	}
	return entries
}
func (h *Handler) maskHistoryIfRequired(ctx context.Context, hostID string, resp *dto.HistoryDetailResponse) {
	if resp == nil {
		return
	}
	canView, _ := h.BillingSvc.CanViewGuestData(ctx, hostID)
	if canView {
		return
	}

	for i := range resp.Entries {
		resp.Entries[i].Email = helpers.MaskEmail(resp.Entries[i].Email)
		resp.Entries[i].Phone = helpers.MaskPhone(resp.Entries[i].Phone)
	}
}
