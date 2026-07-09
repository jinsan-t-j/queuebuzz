package http

import (
	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/system/domain"
	"queuebuzz/internal/modules/system/service"
	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg           *config.Config
	systemService *service.SystemService
	emailService  *services.EmailService
}

func NewHandler(cfg *config.Config, systemSvc *service.SystemService, emailSvc *services.EmailService) *Handler {
	return &Handler{
		cfg:           cfg,
		systemService: systemSvc,
		emailService:  emailSvc,
	}
}

func (h *Handler) GetSettings(c fiber.Ctx) error {
	settings, err := h.systemService.GetSettings(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to fetch system settings")
	}
	return c.JSON(settings)
}

func (h *Handler) UpdateSettings(c fiber.Ctx) error {
	var settings domain.SystemSettings
	if err := c.Bind().JSON(&settings); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.systemService.UpdateSettings(c.Context(), &settings); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update system settings")
	}

	return c.JSON(settings)
}

func (h *Handler) SubmitSupport(c fiber.Ctx) error {
	var req domain.SupportRequest
	if err := c.Bind().JSON(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.emailService.SendSupportRequest(req.Name, req.Email, req.Subject, req.Message); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to send support email")
	}

	return c.JSON(fiber.Map{
		"message": "Support request submitted successfully",
	})
}
