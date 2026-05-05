package http

import (
	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/system/domain"
	"queuebuzz/internal/modules/system/service"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg           *config.Config
	systemService *service.SystemService
}

func NewHandler(cfg *config.Config, systemSvc *service.SystemService) *Handler {
	return &Handler{
		cfg:           cfg,
		systemService: systemSvc,
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
