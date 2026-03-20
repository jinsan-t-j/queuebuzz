package middlewares

import (
	"context"
	"time"

	"queuebuzz/internal/constants"
	authservice "queuebuzz/internal/modules/auth/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	hostOwnerAuthSvc  *authservice.AuthService
	hostOwnerRedisSvc *legacyservices.RedisService
	queueCollection   *mongo.Collection
)

// InitHostOwnerMiddleware sets up depenencies for the host owner middleware.
func InitHostOwnerMiddleware(authSvc *authservice.AuthService, redisSvc *legacyservices.RedisService, queueCol *mongo.Collection) {
	hostOwnerAuthSvc = authSvc
	hostOwnerRedisSvc = redisSvc
	queueCollection = queueCol
}

// HostOwnerMiddleware verifies that the authenticated host owns the queue in :id.
// Works for both anonymous hosts (SHA256 hash check) and registered hosts (host_id match).
func HostOwnerMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		role, _ := c.Locals("role").(string)
		queueID := c.Params("id")

		if queueID == "" {
			return fiber.NewError(fiber.StatusForbidden)
		}

		switch role {
		case constants.RoleAnonymousHost:
			claimQueueID, _ := c.Locals("queue_id").(string)
			if claimQueueID != queueID {
				return fiber.NewError(fiber.StatusForbidden, claimQueueID+" "+queueID)
			}

			// Verify SHA256(jwt) exists in Redis owner:{queue_id}
			rawToken, _ := c.Locals("raw_access_token").(string)
			if err := hostOwnerAuthSvc.VerifyAnonymousOwnership(c.Context(), rawToken, queueID); err != nil {
				return fiber.NewError(fiber.StatusForbidden, err.Error())
			}

		case constants.RoleRegisteredHost:
			hostID, _ := c.Locals("host_id").(string)

			ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
			defer cancel()

			var result struct {
				HostID *string `bson:"host_id"`
			}
			err := queueCollection.FindOne(ctx, bson.M{"_id": queueID}).Decode(&result)
			if err != nil {
				return fiber.NewError(fiber.StatusForbidden)
			}

			if result.HostID == nil || *result.HostID != hostID {
				return fiber.NewError(fiber.StatusForbidden)
			}

		default:
			return fiber.NewError(fiber.StatusForbidden)
		}

		return c.Next()
	}
}
