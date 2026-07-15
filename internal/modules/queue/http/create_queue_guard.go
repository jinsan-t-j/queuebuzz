package http

import (
	"errors"
	"os"

	"queuebuzz/internal/modules/billing/service"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

var errResponded = errors.New("response already sent")

func CreateQueueGuard(billingSvc *service.BillingService, queueSvc *queueservice.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		hostID, _ := c.Locals("host_id").(string)
		hostPublicID, _ := c.Locals("host_public_id").(string)
		queueID, _ := c.Locals("queue_id").(string)

		if os.Getenv("DISABLE_ACTIVE_QUEUE_GUARD") != "true" && os.Getenv("DISABLE_BILLING_GUARDS") != "true" {
			if err := checkActiveQueue(c, queueSvc, hostPublicID, queueID); err != nil {
				return nil
			}
		}

		if err := checkMonthlyQuota(c, billingSvc, hostID); err != nil {
			return nil
		}

		return c.Next()
	}
}

func checkActiveQueue(c fiber.Ctx, svc *queueservice.Service, hostPublicID, queueID string) error {
	switch {
	case hostPublicID != "":
		q, err := svc.GetLiveQueueForHost(c.Context(), hostPublicID)
		if err == nil && q != nil {
			_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "You already have an active queue. Please close or terminate it before starting a new one.",
				"code":     "ACTIVE_QUEUE_EXISTS",
				"queue_id": q.ID,
			})
			return errResponded
		}

	case queueID != "":
		q, err := svc.GetLiveQueueByID(c.Context(), queueID)
		if err == nil && q != nil {
			_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "You already have a live queue in this session. Sign in to save your history or close it to start a new one.",
				"code":     "ACTIVE_QUEUE_EXISTS",
				"queue_id": q.ID,
			})
			return errResponded
		}
	}

	return nil
}

func checkMonthlyQuota(c fiber.Ctx, svc *service.BillingService, hostID string) error {
	if hostID == "" {
		return nil
	}

	exceeded, err := svc.IsLimitExceeded(c.Context(), hostID, "queues_monthly", -1)
	if err != nil {
		_ = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Unable to verify queue limits. Please try again.",
			"code":  "LIMIT_CHECK_FAILED",
		})
		return errResponded
	}

	if exceeded {
		_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Monthly queue limit reached. Please upgrade your plan to add more.",
			"code":  "MONTHLY_LIMIT_EXCEEDED",
		})
		return errResponded
	}

	return nil
}
