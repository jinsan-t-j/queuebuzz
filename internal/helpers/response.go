package helpers

import (
	"github.com/gofiber/fiber/v3"
)

type SuccessResponse struct {
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func NewSuccessResponse(message string, data interface{}) *SuccessResponse {
	return &SuccessResponse{
		Message: message,
		Data:    data,
	}
}

func (sc SuccessResponse) OK(c fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(sc)
}

func (sc SuccessResponse) Created(c fiber.Ctx) error {
	return c.Status(fiber.StatusCreated).JSON(sc)
}

func (sc SuccessResponse) MessageResponse(c fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(SuccessResponse{
		Message: sc.Message,
	})
}

type ErrorResponse struct {
	Message string `json:"message"`
	Error   any    `json:"error,omitempty"`
}

func (e ErrorResponse) JSON(c fiber.Ctx, status int) error {
	return c.Status(status).JSON(fiber.Map{
		"message": e.Message,
		"error":   e.Error,
	})
}
