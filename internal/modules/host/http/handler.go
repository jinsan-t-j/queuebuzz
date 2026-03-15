package http

import (
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	authservice "queuebuzz/internal/modules/auth/service"
	hostservice "queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg          *config.Config
	authService  *authservice.AuthService
	redisService *legacyservices.RedisService
	hostService  *hostservice.Service
	queueService *queueservice.Service
}

func NewHandler(cfg *config.Config, authSvc *authservice.AuthService, redisSvc *legacyservices.RedisService, hostSvc *hostservice.Service, queueSvc *queueservice.Service) *Handler {
	return &Handler{
		cfg:          cfg,
		authService:  authSvc,
		redisService: redisSvc,
		hostService:  hostSvc,
		queueService: queueSvc,
	}
}

func (h *Handler) Claim(c fiber.Ctx) error {
	registeredHostID, _ := c.Locals("host_id").(string)
	if registeredHostID == "" {
		return fiber.NewError(fiber.StatusForbidden)
	}

	ownerCookie := c.Cookies("owner_token")
	if ownerCookie == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing owner token cookie")
	}

	anonClaims, err := h.authService.VerifyToken(ownerCookie)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized)
	}
	if anonClaims.Role != constants.RoleAnonymousHost || anonClaims.QueueID == "" {
		return fiber.NewError(fiber.StatusForbidden)
	}
	if err := h.authService.VerifyAnonymousOwnership(c.Context(), ownerCookie, anonClaims.QueueID); err != nil {
		return fiber.NewError(fiber.StatusForbidden)
	}

	host, err := h.hostService.FindByID(c.Context(), registeredHostID)
	if err != nil {
		return fiber.NewError(fiber.StatusForbidden)
	}
	if _, err := h.queueService.GetQueue(c.Context(), anonClaims.QueueID); err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}
	if err := h.hostService.ClaimQueue(c.Context(), anonClaims.QueueID, registeredHostID, host.PublicID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	_ = h.redisService.DeleteOwnerToken(c.Context(), anonClaims.QueueID)
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Queue claimed successfully"})
}

func (h *Handler) Logout(c fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken != "" {
		_ = h.redisService.DeleteRefreshToken(c.Context(), refreshToken)
	}

	for _, name := range []string{"access_token", "refresh_token"} {
		c.Cookie(&fiber.Cookie{
			Name:     name,
			Value:    "",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HTTPOnly: true,
			Secure:   h.cfg.IsProduction(),
			SameSite: "Lax",
			Path:     "/",
		})
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Signed out successfully"})
}

func (h *Handler) GetMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}
	host, err := h.hostService.FindByID(c.Context(), hostID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}

	email := ""
	if host.Email != nil {
		email = *host.Email
	}
	name := host.PublicID
	if email != "" {
		name = email
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":            host.ID,
		"public_id":     host.PublicID,
		"name":          name,
		"email":         email,
		"business_name": "QueueBuzz Host",
		"tier":          host.Tier,
		"avatar":        nil,
	})
}

func (h *Handler) GetProfile(c fiber.Ctx) error {
	host, err := h.hostService.FindByPublicID(c.Context(), c.Params("public_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":         host.ID,
		"public_id":  host.PublicID,
		"tier":       host.Tier,
		"created_at": host.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) GetQueues(c fiber.Ctx) error {
	queues, err := h.queueService.GetActiveQueuesForHost(c.Context(), c.Params("public_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(queues)
}
