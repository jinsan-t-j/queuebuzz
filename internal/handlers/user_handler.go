package handlers

import (
	"sync"

	"queuebuzz/internal/requests"
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

// AddEmail godoc
// @Summary Add email to queue entry
// @Description Adds an optional email to a user's queue entry after they have joined
// @Tags User
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param X-User-Token header string true "User Token"
// @Param request body requests.AddEmailRequest true "Email Request"
// @Success 200 {string} string "OK"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/user/email [post]
func (h *UserHandler) AddEmail(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	var req requests.AddEmailRequest
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

// SetPIN godoc
// @Summary Set PIN for queue entry
// @Description Sets or updates a 4-digit recovery PIN for a queue entry
// @Tags User
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param X-User-Token header string true "User Token"
// @Param request body requests.SetPINRequest true "PIN Request"
// @Success 200 {string} string "OK"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /queues/{id}/user/pin [post]
func (h *UserHandler) SetPIN(c fiber.Ctx) error {
	queueID := c.Params("id")
	token := c.Get("X-User-Token")

	if token == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	var req requests.SetPINRequest
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

// Rejoin godoc
// @Summary Rejoin a queue session
// @Description Restores a user session by token (primary) or by ticket number and PIN (fallback)
// @Tags User
// @Accept json
// @Produce json
// @Param id path string true "Queue ID"
// @Param X-User-Token header string false "User Token"
// @Param request body requests.RejoinPINRequest false "Rejoin Request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Router /queues/{id}/user/rejoin [post]
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
	var req requests.RejoinPINRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	result, err := h.queueService.RejoinByPIN(c.Context(), queueID, req.TicketNo, req.PIN)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(result)
}
