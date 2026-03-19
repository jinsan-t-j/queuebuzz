package http

import (
	customerdto "queuebuzz/internal/modules/customer/dto"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	queueService *queueservice.Service
	redisService *legacyservices.RedisService
}

func NewHandler(queueSvc *queueservice.Service, redisSvc *legacyservices.RedisService) *Handler {
	return &Handler{queueService: queueSvc, redisService: redisSvc}
}

func (h *Handler) AddEmail(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")
	if token == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var req customerdto.AddEmailRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, token)
	if err != nil || !exists {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.queueService.SetUserEmail(c.Context(), queueID, token, req.Email); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) SetPIN(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")
	if token == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var req customerdto.SetPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, token)
	if err != nil || !exists {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.queueService.SetUserPIN(c.Context(), queueID, token, req.PIN); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) Rejoin(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")
	if token != "" {
		result, err := h.queueService.RejoinByToken(c.Context(), queueID, token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		return c.Status(fiber.StatusOK).JSON(result)
	}
	var req customerdto.RejoinPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	result, err := h.queueService.RejoinByPIN(c.Context(), queueID, req.TicketNo, req.PIN)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	return c.Status(fiber.StatusOK).JSON(result)
}
