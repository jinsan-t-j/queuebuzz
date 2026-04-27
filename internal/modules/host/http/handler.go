package http

import (
	"fmt"
	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	authservice "queuebuzz/internal/modules/auth/service"
	"queuebuzz/internal/modules/host/dto"
	hostservice "queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/services/storage"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg          *config.Config
	authService  *authservice.AuthService
	redisService *legacyservices.RedisService
	hostService  *hostservice.Service
	queueService *queueservice.Service
	r2Service    *storage.R2Service
}

func NewHandler(
	cfg *config.Config,
	authSvc *authservice.AuthService,
	redisSvc *legacyservices.RedisService,
	hostSvc *hostservice.Service,
	queueSvc *queueservice.Service,
	r2Svc *storage.R2Service,
) *Handler {
	return &Handler{
		cfg:          cfg,
		authService:  authSvc,
		redisService: redisSvc,
		hostService:  hostSvc,
		queueService: queueSvc,
		r2Service:    r2Svc,
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
		ID:              host.ID,
		PublicID:        host.PublicID,
		Name:            name,
		Email:           email,
		Tier:            host.Tier,
		Avatar:          "",
		ProfileImageURL: helpers.DerefString(host.ProfileImageURL),
		BannerImageURL:  helpers.DerefString(host.BannerImageURL),
	}).OK(c)
}

func (h *Handler) UpdateMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	var updates bson.M
	if err := c.Bind().JSON(&updates); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	// Filter out sensitive/internal fields from updates if necessary
	delete(updates, "_id")
	delete(updates, "id")
	delete(updates, "public_id")
	delete(updates, "created_at")

	const maxImageBytes = 1024 * 1024 // 1MB
	ctx := c.Context()

	// Handle Profile Image
	if v, ok := updates["profile_image_url"]; ok {
		val, _ := v.(string)
		if val == "" {
			updates["profile_image_url"] = nil
		} else if strings.HasPrefix(val, "data:") {
			if err := validateImageDataURL(val, maxImageBytes); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "profile image: "+err.Error())
			}
			data, contentType, err := ParseImageDataURL(val)
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "failed to parse profile image")
			}
			ext := ".png"
			if strings.Contains(contentType, "jpeg") {
				ext = ".jpg"
			} else if strings.Contains(contentType, "webp") {
				ext = ".webp"
			} else if strings.Contains(contentType, "gif") {
				ext = ".gif"
			}
			key := h.r2Service.GenerateKey(hostID, fmt.Sprintf("profile_%d", time.Now().Unix()), ext)
			url, err := h.r2Service.Upload(ctx, key, data, contentType)
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, "failed to upload profile image")
			}
			updates["profile_image_url"] = url
		}
	}

	// Handle Banner Image
	if v, ok := updates["banner_image_url"]; ok {
		val, _ := v.(string)
		if val == "" {
			updates["banner_image_url"] = nil
		} else if strings.HasPrefix(val, "data:") {
			if err := validateImageDataURL(val, maxImageBytes); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "banner image: "+err.Error())
			}
			data, contentType, err := ParseImageDataURL(val)
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "failed to parse banner image")
			}
			ext := ".png"
			if strings.Contains(contentType, "jpeg") {
				ext = ".jpg"
			} else if strings.Contains(contentType, "webp") {
				ext = ".webp"
			} else if strings.Contains(contentType, "gif") {
				ext = ".gif"
			}
			key := h.r2Service.GenerateKey(hostID, fmt.Sprintf("banner_%d", time.Now().Unix()), ext)
			url, err := h.r2Service.Upload(ctx, key, data, contentType)
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, "failed to upload banner image")
			}
			updates["banner_image_url"] = url
		}
	}

	if err := h.hostService.UpdateHost(ctx, hostID, updates); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update profile")
	}

	return helpers.NewSuccessResponse("Profile updated successfully", nil).OK(c)
}

func (h *Handler) DeleteMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	if err := h.hostService.DeleteHost(c.Context(), hostID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete account")
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

	return helpers.NewSuccessResponse("Account deleted successfully", nil).OK(c)
}
