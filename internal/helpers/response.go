package helpers

import (
	"github.com/gofiber/fiber/v3"
)

// SuccessResponse is the standard envelope for successful API responses.
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

// OK sends a 200 response with data.
func OK(c fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(SuccessResponse{
		Success: true,
		Data:    data,
	})
}

// Created sends a 201 response with data.
func Created(c fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(SuccessResponse{
		Success: true,
		Data:    data,
	})
}

// Message sends a 200 response with a message string only.
func Message(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusOK).JSON(SuccessResponse{
		Success: true,
		Message: msg,
	})
}
