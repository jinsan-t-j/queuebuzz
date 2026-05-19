package http

import (
	"queuebuzz/internal/constants"
	"queuebuzz/internal/modules/billing/service"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

/**
 * @middleware GuestCapacityGuard
 * @description Ensures the queue hasn't exceeded its guest limit before allowing new joins.
 */
func GuestCapacityGuard(billingSvc *service.BillingService, queueSvc *queueservice.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		queueID := c.Params("id")
		if queueID == "" {
			return c.Next()
		}

		queue, err := queueSvc.GetQueue(c.Context(), queueID)
		if err != nil {
			return c.Next() // Service will handle 404
		}

		if queue.Status == constants.QueueStatusPaused {
			isHost := c.Locals("host_id") != nil
			if !isHost {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "This queue is currently not accepting new entries. Please check again later.",
					"code":  "QUEUE_PAUSED",
				})
			}
		}

		hostID := ""
		if queue.HostID != nil {
			hostID = *queue.HostID
		}

		currentCount, _ := queueSvc.GetWaitingCount(c.Context(), queueID)
		exceeded, err := billingSvc.IsLimitExceeded(c.Context(), hostID, "guests", int(currentCount+1))
		if err == nil && exceeded {
			// Determine if the request is from a host (admin add) or guest (public join)
			isHost := c.Locals("host_id") != nil
			if isHost {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "This queue has reached its maximum capacity for your current plan. Please upgrade to add more guests.",
					"code":  "GUEST_LIMIT_EXCEEDED",
				})
			}
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "This queue is currently full. Please contact the business owner or try again later.",
				"code":  "QUEUE_FULL",
			})
		}

		// Store queue in locals to avoid double-fetching in the handler/service
		c.Locals("queue_context", queue)
		return c.Next()
	}
}
