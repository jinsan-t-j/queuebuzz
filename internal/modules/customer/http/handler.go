package http

import (
	"context"
	"encoding/json"
	"time"

	"queuebuzz/internal/helpers"
	customerdto "queuebuzz/internal/modules/customer/dto"
	customerevents "queuebuzz/internal/modules/customer/events"
	customerservice "queuebuzz/internal/modules/customer/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	customerService *customerservice.Service
	redisService    *legacyservices.RedisService
	broker          *sse.Broker
}

func NewHandler(customerSvc *customerservice.Service, redisSvc *legacyservices.RedisService, broker *sse.Broker) *Handler {
	return &Handler{
		customerService: customerSvc,
		redisService:    redisSvc,
		broker:          broker,
	}
}

func (h *Handler) GetEntry(c fiber.Ctx) error {
	entryID := c.Locals("entry_id").(string)
	entry, err := h.customerService.GetEntry(c.Context(), entryID)

	pos, err := h.redisService.GetPosition(c.Context(), entry.QueueID, entryID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Ticket not found")
	}
	return helpers.NewSuccessResponse("Entry fetched", customerdto.ToEntryResponse(*entry, pos)).OK(c)
}

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

func (h *Handler) AddEmail(c fiber.Ctx) error {
	queueID := c.Locals("entry_id").(string)
	entryID := c.Get("X-User-Token")
	if entryID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var req customerdto.AddEmailRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, entryID)
	if err != nil || !exists {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.customerService.SetUserEmail(c.Context(), entryID, req.Email); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) SetPIN(c fiber.Ctx) error {
	queueID := c.Params("id")
	entryID := c.Get("X-User-Token")
	if entryID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var req customerdto.SetPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, entryID)
	if err != nil || !exists {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.customerService.SetUserPIN(c.Context(), entryID, req.PIN); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) Rejoin(c fiber.Ctx) error {
	queueID := c.Params("id")
	entryID := c.Get("X-User-Token")
	if entryID != "" {
		result, err := h.customerService.RejoinByID(c.Context(), entryID)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		response := customerdto.StatusResponse{
			Entry: customerdto.ToEntryResponse(result.Entry, result.Position),
		}
		return c.Status(fiber.StatusOK).JSON(response)
	}
	var req customerdto.RejoinPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	result, err := h.customerService.RejoinByPIN(c.Context(), queueID, req.TicketNo, req.PIN)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	response := customerdto.StatusResponse{
		Entry: customerdto.ToEntryResponse(result.Entry, result.Position),
	}
	return c.Status(fiber.StatusOK).JSON(response)
}
