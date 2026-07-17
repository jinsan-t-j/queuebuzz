package http

import (
	"crypto/subtle"
	"os"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/exceptions"
	"queuebuzz/internal/modules/billing/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	rdbpkg "queuebuzz/internal/redis"

	"github.com/gofiber/fiber/v3"
	redisdriver "github.com/redis/go-redis/v9"
)

func CreateQueueGuard(
	cfg *config.Config,
	rdb *redisdriver.Client,
	billingSvc *service.BillingService,
	queueSvc *queueservice.Service,
) fiber.Handler {
	return func(c fiber.Ctx) error {
		hostID, _ := c.Locals("host_id").(string)
		hostPublicID, _ := c.Locals("host_public_id").(string)
		queueID, _ := c.Locals("queue_id").(string)

		var lockKey string
		if hostID != "" {
			lockKey = "lock:create-queue:host:" + hostID
		} else if hostPublicID != "" {
			lockKey = "lock:create-queue:public-host:" + hostPublicID
		} else if queueID != "" {
			lockKey = "lock:create-queue:session:" + queueID
		}

		if lockKey != "" {
			lock := rdbpkg.NewLock(rdb, lockKey)
			acquired, err := lock.Acquire(c.Context(), 10*time.Second)
			if err != nil || !acquired {
				_ = c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "A queue creation request is already in progress. Please wait.",
					"code":  "CONCURRENT_CREATION",
				})
				return exceptions.ErrResponded
			}
			defer func() {
				_ = lock.Release(c.Context())
			}()
		}

		bypassToken := cfg.OverrideQueueGuardToken
		if bypassToken == "" {
			bypassToken = os.Getenv("OVERIDE_QUEUE_GUARD_TOKEN")
		}
		clientToken := c.Get("X-Bypass-Active-Queue-Guard")
		shouldBypass := bypassToken != "" && clientToken != "" &&
			subtle.ConstantTimeCompare([]byte(bypassToken), []byte(clientToken)) == 1

		if !shouldBypass {
			if err := checkActiveQueue(c, queueSvc, hostPublicID, queueID); err != nil {
				return nil
			}
			if err := checkMonthlyQuota(c, billingSvc, hostID); err != nil {
				return nil
			}
		}

		return c.Next()
	}
}

func checkActiveQueue(c fiber.Ctx, svc *queueservice.Service, hostPublicID, queueID string) error {
	switch {
	case hostPublicID != "":
		activeQueueID, err := svc.HasActiveQueue(c.Context(), hostPublicID, "")
		if err == nil && activeQueueID != "" {
			_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "You already have an active queue. Please close or terminate it before starting a new one.",
				"code":     "ACTIVE_QUEUE_EXISTS",
				"queue_id": activeQueueID,
			})
			return exceptions.ErrResponded
		}

	case queueID != "":
		activeQueueID, err := svc.HasActiveQueue(c.Context(), "", queueID)
		if err == nil && activeQueueID != "" {
			_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":    "You already have a live queue in this session. Sign in to save your history or close it to start a new one.",
				"code":     "ACTIVE_QUEUE_EXISTS",
				"queue_id": activeQueueID,
			})
			return exceptions.ErrResponded
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
			"error": "Unable to verify queue limits. Please try again. ",
			"code":  "LIMIT_CHECK_FAILED",
		})
		return exceptions.ErrResponded
	}

	if exceeded {
		_ = c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Monthly queue limit reached. Please upgrade your plan to add more.",
			"code":  "MONTHLY_LIMIT_EXCEEDED",
		})
		return exceptions.ErrResponded
	}

	return nil
}
