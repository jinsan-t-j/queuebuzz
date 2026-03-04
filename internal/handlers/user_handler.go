package handlers

import (
	"sync"

	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

var (
	userHandlerInstance *UserHandler
	userHandlerOnce     sync.Once
)

type UserHandler struct {
	queueService *services.QueueService
	redisService *services.RedisService
}

func NewUserHandler(queueSvc *services.QueueService, redisSvc *services.RedisService) *UserHandler {
	userHandlerOnce.Do(func() {
		userHandlerInstance = &UserHandler{
			queueService: queueSvc,
			redisService: redisSvc,
		}
	})

	return userHandlerInstance
}

type addEmailRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// AddEmail adds an optional email to a user's queue entry (post-join).
func (h *UserHandler) AddEmail(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	var req addEmailRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	// Verify user session
	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, token)
	if err != nil || !exists {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	if err := h.queueService.SetUserEmail(c.Context(), queueID, token, req.Email); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

type setPINRequest struct {
	PIN string `json:"pin" validate:"required,len=4"`
}

// SetPIN sets or updates a recovery PIN for a queue entry.
func (h *UserHandler) SetPIN(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	var req setPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	exists, err := h.redisService.UserSessionExists(c.Context(), queueID, token)
	if err != nil || !exists {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	if err := h.queueService.SetUserPIN(c.Context(), queueID, token, req.PIN); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

type rejoinPINRequest struct {
	TicketNo string `json:"ticket_no" validate:"required"`
	PIN      string `json:"pin" validate:"required,len=4"`
}

// Rejoin restores a user session — by token (primary) or by ticket_no + PIN (fallback).
func (h *UserHandler) Rejoin(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	// Primary path — rejoin by token
	if token != "" {
		result, err := h.queueService.RejoinByToken(c.Context(), queueID, token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		return c.Status(fiber.StatusOK).JSON(result)
	}

	// Fallback path — rejoin by ticket_no + PIN
	var req rejoinPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	result, err := h.queueService.RejoinByPIN(c.Context(), queueID, req.TicketNo, req.PIN)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(result)
}
