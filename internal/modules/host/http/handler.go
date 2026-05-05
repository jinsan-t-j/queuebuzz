package http

import (
	"encoding/json"
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
	emailService *legacyservices.EmailService
}

func NewHandler(
	cfg *config.Config,
	authSvc *authservice.AuthService,
	redisSvc *legacyservices.RedisService,
	hostSvc *hostservice.Service,
	queueSvc *queueservice.Service,
	r2Svc *storage.R2Service,
	emailSvc *legacyservices.EmailService,
) *Handler {
	return &Handler{
		cfg:          cfg,
		authService:  authSvc,
		redisService: redisSvc,
		hostService:  hostSvc,
		queueService: queueSvc,
		r2Service:    r2Svc,
		emailService: emailSvc,
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

// UpdateMe godoc
// @Summary Update authenticated host
// @Description Updates the authenticated host profile.
// @Tags Host
// @Produce json
// @Success 200 {object} map[string]interface{} "Updated Host profile"
// @Param host body dto.UpdateMeRequest true "Host profile updates"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/me [patch]
func (h *Handler) UpdateMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	var updates = make(bson.M)
	contentType := c.Get("Content-Type")

	// 1. Parse Primary Payload
	if strings.Contains(contentType, fiber.MIMEApplicationJSON) {
		if err := c.Bind().JSON(&updates); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
	} else if strings.Contains(contentType, fiber.MIMEMultipartForm) {
		// Handle multipart form fields
		form, err := c.MultipartForm()
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "failed to parse form data")
		}
		for k, v := range form.Value {
			if len(v) > 0 {
				if k == "settings" {
					var settings bson.M
					if err := json.Unmarshal([]byte(v[0]), &settings); err == nil {
						updates["settings"] = settings
					}
				} else {
					updates[k] = v[0]
				}
			}
		}
	}

	// Filter internal fields
	delete(updates, "_id")
	delete(updates, "id")
	delete(updates, "public_id")
	delete(updates, "created_at")

	const maxImageBytes = 1024 * 1024 // 1MB
	ctx := c.Context()

	// 2. Process Images (Multipart Files take priority over Base64)
	imageFields := []struct {
		formName  string
		dbField   string
		typeLabel string
	}{
		{"profile_image", "profile_image_url", "profile"},
		{"banner_image", "banner_image_url", "banner"},
	}

	for _, field := range imageFields {
		// 1. Check for multipart file upload
		fh, err := c.FormFile(field.formName)
		if err == nil {
			if err := validateImageFile(fh, maxImageBytes); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, field.formName+": "+err.Error())
			}
			f, err := fh.Open()
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, "failed to open "+field.formName)
			}
			data := make([]byte, fh.Size)
			if _, err := f.Read(data); err != nil {
				f.Close()
				return fiber.NewError(fiber.StatusInternalServerError, "failed to read "+field.formName)
			}
			f.Close()

			mime := fh.Header.Get("Content-Type")
			ext := GetExtensionFromMIME(mime)
			key := h.r2Service.GenerateKey(hostID, field.typeLabel, ext)
			url := ""
			url, err = h.r2Service.Upload(ctx, key, data, mime)
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, "failed to upload "+field.formName)
			}
			updates[field.dbField] = url
		} else if v, ok := updates[field.dbField]; ok {
			// 2. Check for image clearing (empty string)
			val, _ := v.(string)
			if val == "" {
				updates[field.dbField] = nil
			} else {
				// We no longer support updating images via JSON/Base64 strings.
				// If a URL is passed but no file is provided, we ignore the field.
				delete(updates, field.dbField)
			}
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

	host, _ := h.hostService.FindByID(c.Context(), hostID)

	if err := h.hostService.DeleteHost(c.Context(), hostID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete account")
	}

	if host != nil && host.Email != nil {
		go h.emailService.SendAccountDeletionEmail(*host.Email, host.Name)
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
