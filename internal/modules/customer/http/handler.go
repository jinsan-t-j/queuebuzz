package http

import (
	"context"
	"encoding/json"
	"time"

	"queuebuzz/internal/helpers"
	authservice "queuebuzz/internal/modules/auth/service"
	customerdto "queuebuzz/internal/modules/customer/dto"
	customerevents "queuebuzz/internal/modules/customer/events"
	customerservice "queuebuzz/internal/modules/customer/service"
	queuedto "queuebuzz/internal/modules/queue/dto"
	queueresource "queuebuzz/internal/modules/queue/jobs"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Handler struct {
	customerService *customerservice.Service
	queueService    *queueservice.Service
	authService     *authservice.AuthService
	joinCodeService *legacyservices.JoinCodeService
	broker          *sse.Broker
	broadcaster     *queueresource.Broadcaster
}

func NewHandler(
	customerSvc *customerservice.Service,
	queueSvc *queueservice.Service,
	authSvc *authservice.AuthService,
	joinCodeSvc *legacyservices.JoinCodeService,
	broker *sse.Broker,
	broadcaster *queueresource.Broadcaster,
) *Handler {
	return &Handler{
		customerService: customerSvc,
		queueService:    queueSvc,
		authService:     authSvc,
		joinCodeService: joinCodeSvc,
		broker:          broker,
		broadcaster:     broadcaster,
	}
}

// Leave godoc
// @Summary Leave queue
// @Description Leaves the queue for the authenticated host.
// @Tags Entry
// @Produce json
// @Success 200 {object} helpers.SuccessResponse "Successfully left the queue"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /entry/{id} [get]
func (h *Handler) Leave(c fiber.Ctx) error {
	entryID := c.Locals("entry_id").(string)
	queueID := c.Locals("queue_id").(string)

	if err := h.customerService.Leave(c.Context(), entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to leave queue")
	}

	h.broadcaster.DispatchPositionUpdate(queueID)

	c.Cookie(&fiber.Cookie{
		Name:     "guest_entry_token",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Lax",
		Path:     "/",
	})

	return helpers.NewSuccessResponse("Successfully left the queue", nil).MessageResponse(c)
}

// GetEntry godoc
// @Summary Get entry by ID
// @Description Gets an entry by ID for the authenticated host.
// @Tags Entry
// @Produce json
// @Success 200 {object} queuedto.EntryRecord "Entry added"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /entry/{id} [get]
func (h *Handler) GetEntry(c fiber.Ctx) error {
	entryID := c.Locals("entry_id").(string)
	entry, err := h.customerService.GetEntry(c.Context(), entryID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Entry not found")
	}

	pos, err := h.queueService.GetPosition(c.Context(), entry.QueueID, entryID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Ticket not found")
	}
	return helpers.NewSuccessResponse("Entry fetched", customerdto.ToEntryResponse(*entry, pos+1)).OK(c)
}

// StreamEvents godoc
// @Summary Stream events for an entry
// @Description Streams events for an entry.
// @Tags Entry
// @Produce json
// @Success 200 {object} queuedto.EntryRecord "Entry added"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /entry/{id}/stream [get]
func (h *Handler) StreamEvents(c fiber.Ctx) error {
	entryID := c.Locals("entry_id").(string)
	topic := "entry:" + entryID

	snapshotFn := func() ([][]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := h.customerService.GetEntryStatusByID(ctx, entryID)
		if err != nil {
			return nil, err
		}

		statusMsg := customerevents.Wrap(customerevents.EventEntryStatusChanged, customerevents.EntryStatusChangedData{
			Status: result,
		})
		initPayload, _ := json.Marshal(statusMsg)

		return [][]byte{initPayload}, nil
	}

	return h.broker.ServeHTTP(c, topic, snapshotFn)
}

// UpdateEntry godoc
// @Summary Update entry details
// @Description Updates the name, email, or party size for the currently authenticated entry.
// @Tags Entry
// @Produce json
// @Param request body customerdto.UpdateEntryRequest true "Update entry request"
// @Success 200 {object} map[string]string "Success message"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /entry/update [post]
func (h *Handler) UpdateEntry(c fiber.Ctx) error {
	entryID := c.Locals("entry_id").(string)
	queueID := c.Locals("queue_id").(string)

	var req customerdto.UpdateEntryRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	exists, err := h.customerService.SessionExists(c.Context(), queueID, entryID)
	if err != nil || !exists {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized session"})
	}

	updates := make(bson.M)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	if req.PartySize != nil {
		updates["party_size"] = *req.PartySize
	}

	if len(updates) == 0 {
		return helpers.NewSuccessResponse("No changes applied", nil).OK(c)
	}

	if err := h.customerService.UpdateEntry(c.Context(), entryID, updates); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update entry")
	}

	// Broadcaster should notify the host of name/party-size change if relevant
	// Actually, for simplicity, we just return OK. The host will reflect changes on next refresh or event.

	return helpers.NewSuccessResponse("Entry updated successfully", nil).OK(c)
}

// JoinByQueueID godoc
// @Summary Join queue by ID
// @Description Joins the queue for the authenticated host.
// @Tags Entry
// @Produce json
// @Success 200 {object} queuedto.EntryRecord "Successfully joined the queue"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /entry/join/{id} [post]
func (h *Handler) JoinByQueueID(c fiber.Ctx) error {
	var req queuedto.JoinRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	result, err := h.customerService.JoinQueue(c.Context(), queueservice.JoinQueueParams{
		QueueID:   c.Params("id"),
		FCMToken:  req.FCMToken,
		Name:      *req.DisplayName,
		Email:     req.Email,
		Phone:     req.Phone,
		PIN:       req.PIN,
		PartySize: req.PartySize,
	})

	if err != nil {
		return err
	}

	h.issueGuestToken(c, result.Entry.QueueID, result.Entry.ID)

	return helpers.NewSuccessResponse("Successfully joined the queue", queuedto.ToEntryResponse(result.Entry, result.Position)).OK(c)
}

func (h *Handler) JoinByCode(c fiber.Ctx) error {
	var req queuedto.JoinByCodeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	queueID, err := h.joinCodeService.ResolveJoinCode(c.Context(), req.JoinCode)
	if err != nil || queueID == "" {
		// Fallback to database
		queue, dbErr := h.queueService.GetQueueByJoinCode(c.Context(), req.JoinCode)
		if dbErr == nil && queue != nil {
			queueID = queue.ID
		} else {
			return fiber.NewError(fiber.StatusNotFound, "Invalid or expired queue code")
		}
	}

	result, err := h.customerService.JoinQueue(c.Context(), queueservice.JoinQueueParams{
		QueueID:   queueID,
		FCMToken:  req.FCMToken,
		Name:      *req.DisplayName,
		Email:     req.Email,
		Phone:     req.Phone,
		PIN:       req.PIN,
		PartySize: req.PartySize,
	})

	if err != nil {
		return err
	}

	h.issueGuestToken(c, result.Entry.QueueID, result.Entry.ID)

	return helpers.NewSuccessResponse("Successfully joined the queue", queuedto.ToEntryResponse(result.Entry, result.Position)).OK(c)
}

func (h *Handler) ResolveCode(c fiber.Ctx) error {
	code := c.Params("code")
	queueID, err := h.joinCodeService.ResolveJoinCode(c.Context(), code)
	if err != nil || queueID == "" {
		// Fallback to database
		queue, dbErr := h.queueService.GetQueueByJoinCode(c.Context(), code)
		if dbErr == nil && queue != nil {
			queueID = queue.ID
		} else {
			return fiber.NewError(fiber.StatusNotFound, "Invalid or expired queue code")
		}
	}

	queue, err := h.queueService.GetQueue(c.Context(), queueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Queue not found")
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"queue_id":   queue.ID,
		"queue_name": queue.Name,
	})
}

func (h *Handler) Heartbeat(c fiber.Ctx) error {
	queueID := c.Locals("queue_id").(string)
	entryID := c.Locals("entry_id").(string)

	if err := h.customerService.Heartbeat(c.Context(), queueID, entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update heartbeat")
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) ConfirmArrived(c fiber.Ctx) error {
	queueID := c.Locals("queue_id").(string)
	entryID := c.Locals("entry_id").(string)

	if err := h.customerService.MarkArrived(c.Context(), queueID, entryID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to confirm arrival")
	}
	return helpers.NewSuccessResponse("Arrival confirmed", nil).OK(c)
}

func (h *Handler) RecoverSession(c fiber.Ctx) error {
	cookie := c.Cookies("guest_entry_token")
	if cookie == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "No active queue session found")
	}

	claims, err := h.authService.VerifyToken(cookie)
	if err != nil {
		h.clearGuestToken(c)
		return helpers.NewSuccessResponse("Invalid session cleared", nil).OK(c)
	}

	result, err := h.customerService.RejoinByID(c.Context(), claims.EntryID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Queue entry no longer exists or has expired")
	}

	response := queuedto.ToEntryResponse(result.Entry, result.Position)
	return helpers.NewSuccessResponse("Session recovered", response).OK(c)
}

func (h *Handler) issueGuestToken(c fiber.Ctx, queueID, entryID string) {
	guestToken, err := h.authService.IssueGuestEntryToken(queueID, entryID)
	if err == nil {
		c.Cookie(&fiber.Cookie{
			Name:     "guest_entry_token",
			Value:    guestToken,
			Expires:  time.Now().Add(24 * time.Hour),
			HTTPOnly: true,
			Secure:   true,
			SameSite: "Lax",
			Path:     "/",
		})
	}
}

func (h *Handler) clearGuestToken(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "guest_entry_token",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Lax",
		Path:     "/",
	})
}
