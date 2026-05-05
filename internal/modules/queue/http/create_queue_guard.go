package http

import (
	"queuebuzz/internal/modules/billing/service"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

/**
 * @middleware CreateQueueGuard
 * @description Checks if the host can create a new queue (active limit and monthly quota).
 */
func CreateQueueGuard(billingSvc *service.BillingService, queueSvc *queueservice.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		hostIDRaw := c.Locals("host_id")
		hostPublicIDRaw := c.Locals("host_public_id")

		// If it's an anonymous host, we might skip or apply different logic
		// But usually this guard is for authenticated hosts
		if hostIDRaw == nil {
			return c.Next()
		}

		hostID := hostIDRaw.(string)
		hostPublicID := hostPublicIDRaw.(string)

		// 1. Concurrent Active Limit Check (System Rule: Max 1 active queue per host)
		activeQueue, err := queueSvc.GetLiveQueueForHost(c.Context(), hostPublicID)
		if err == nil && activeQueue != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "You already have an active queue. Please close or terminate it before starting a new one.",
				"code":     "ACTIVE_QUEUE_EXISTS",
				"queue_id": activeQueue.ID,
			})
		}

		// 2. Monthly Quota Check
		exceededMonthly, _ := billingSvc.IsLimitExceeded(c.Context(), hostID, "queues_monthly", -1)
		if exceededMonthly {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "You have reached your monthly queue creation limit. Please upgrade to create more queues.",
				"code":  "MONTHLY_LIMIT_EXCEEDED",
			})
		}

		return c.Next()
	}
}
