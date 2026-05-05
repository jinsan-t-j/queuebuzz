package http

import (
	"queuebuzz/internal/modules/billing/service"

	"github.com/gofiber/fiber/v3"
)

/**
 * @middleware HistoryAccessGuard
 * @description Ensures the host has history access per their billing plan.
 */
func HistoryAccessGuard(billingSvc *service.BillingService) fiber.Handler {
	return func(c fiber.Ctx) error {
		hostID, _ := c.Locals("host_id").(string)
		if hostID == "" {
			hostID, _ = c.Locals("queue_id").(string)
		}

		if hostID == "" {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		hasAccess, err := billingSvc.CheckHistoryAccess(c.Context(), hostID)
		if err != nil || !hasAccess {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Queue history is a premium feature. Please upgrade your plan to access it.",
				"code":  "PREMIUM_REQUIRED",
			})
		}

		return c.Next()
	}
}
