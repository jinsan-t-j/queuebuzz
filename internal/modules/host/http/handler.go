package http

import (
	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	authservice "queuebuzz/internal/modules/auth/service"
	"queuebuzz/internal/modules/host/dto"
	hostservice "queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"time"

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

	anonHostCookie := c.Cookies("queuebuzz_host_token")
	if anonHostCookie == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing anon host token cookie")
	}

	anonClaims, err := h.authService.VerifyToken(anonHostCookie)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if anonClaims.Role != constants.RoleAnonymousHost || anonClaims.QueueID == "" {
		return fiber.NewError(fiber.StatusForbidden)
	}
	if err := h.authService.VerifyAnonymousOwnership(c.Context(), anonHostCookie, anonClaims.QueueID); err != nil {
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

	_ = h.redisService.DeleteAnonHostToken(c.Context(), anonClaims.QueueID)

	c.Cookie(&fiber.Cookie{
		Name:     "queuebuzz_host_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Queue claimed successfully"})
}

// GetMe godoc
// @Summary Get authenticated host
// @Description Gets the authenticated host profile.
// @Tags Host
// @Produce json
// @Success 200 {object} map[string]interface{} "Host profile"
// @Param phone query string false "Phone number"
// @Param otp query string false "OTP code"
// @Success 302 {object} helpers.SuccessResponse{Data=dto.GetMeResponse}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/me [get]
func (h *Handler) GetMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	host, err := h.hostService.FindByID(c.Context(), hostID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}

	email := ""
	if host.Email != nil {
		email = *host.Email
	}
	name := host.Name

	return helpers.NewSuccessResponse("", dto.GetMeResponse{
		ID:       host.ID,
		PublicID: host.PublicID,
		Name:     name,
		Email:    email,
		Tier:     host.Tier,
		Avatar:   "",
	}).OK(c)
}
