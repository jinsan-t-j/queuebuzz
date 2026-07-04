package http

import (
	"encoding/json"
	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	"queuebuzz/internal/log"
	authservice "queuebuzz/internal/modules/auth/service"

	"queuebuzz/internal/modules/billing/service"
	"queuebuzz/internal/modules/host/dto"
	hostservice "queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"
	"queuebuzz/internal/services/storage"
	"strings"
	"time"

	"queuebuzz/internal/validator"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg            *config.Config
	authService    *authservice.AuthService
	redisService   *legacyservices.RedisService
	hostService    *hostservice.Service
	queueService   *queueservice.Service
	billingService *service.BillingService
	r2Service      *storage.R2Service
	emailService   *legacyservices.EmailService
}

func NewHandler(
	cfg *config.Config,
	authSvc *authservice.AuthService,
	redisSvc *legacyservices.RedisService,
	hostSvc *hostservice.Service,
	queueSvc *queueservice.Service,
	billingSvc *service.BillingService,
	r2Svc *storage.R2Service,
	emailSvc *legacyservices.EmailService,
) *Handler {
	return &Handler{
		cfg:            cfg,
		authService:    authSvc,
		redisService:   redisSvc,
		hostService:    hostSvc,
		queueService:   queueSvc,
		billingService: billingSvc,
		r2Service:      r2Svc,
		emailService:   emailSvc,
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

	// Extend queue expiry if the claiming host has a paid account
	if err := h.queueService.ExtendActiveQueueExpiry(c.Context(), anonClaims.QueueID, registeredHostID); err != nil {
		log.Error().Err(err).Str("queue_id", anonClaims.QueueID).Str("host_id", registeredHostID).Msg("Failed to extend claimed queue expiry")
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

	var settingsRes *dto.HostSettingsResponse
	if host.Settings != nil {
		settingsRes = &dto.HostSettingsResponse{
			DefaultQueueName:   host.Settings.DefaultQueueName,
			AvgServiceMins:     host.Settings.AvgServiceMins,
			EmailNotifications: host.Settings.EmailNotifications,
			PushNotifications:  host.Settings.PushNotifications,
		}
	}

	return helpers.NewSuccessResponse("", dto.GetMeResponse{
		ID:              host.ID,
		PublicID:        host.PublicID,
		Slug:            host.Slug,
		Name:            name,
		BusinessName:    host.BusinessName,
		Address:         host.Address,
		Email:           email,
		Phone:           helpers.DerefString(host.Phone),
		Tier:            host.Tier,
		Avatar:          "",
		ProfileImageURL: helpers.DerefString(host.ProfileImageURL),
		BannerImageURL:  helpers.DerefString(host.BannerImageURL),
		Settings:        settingsRes,
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

	var req dto.UpdateMeRequest
	contentType := c.Get("Content-Type")

	// 1. Parse Primary Payload
	if strings.Contains(contentType, fiber.MIMEApplicationJSON) {
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
	} else if strings.Contains(contentType, fiber.MIMEMultipartForm) {
		form, err := c.MultipartForm()
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "failed to parse form data")
		}
		if v := form.Value["name"]; len(v) > 0 {
			req.Name = &v[0]
		}
		if v := form.Value["business_name"]; len(v) > 0 {
			req.BusinessName = &v[0]
		}
		if v := form.Value["address"]; len(v) > 0 {
			req.Address = &v[0]
		}
		if v := form.Value["phone"]; len(v) > 0 {
			req.Phone = &v[0]
		}
		if v := form.Value["profile_image_url"]; len(v) > 0 {
			req.ProfileImageURL = &v[0]
		}
		if v := form.Value["banner_image_url"]; len(v) > 0 {
			req.BannerImageURL = &v[0]
		}
		if v := form.Value["slug"]; len(v) > 0 {
			req.Slug = &v[0]
		}
		if v := form.Value["settings"]; len(v) > 0 {
			var settings dto.HostSettingsRequest
			if err := json.Unmarshal([]byte(v[0]), &settings); err == nil {
				req.Settings = &settings
			} else {
				return fiber.NewError(fiber.StatusBadRequest, "invalid settings format")
			}
		}
	}

	// Check if phone is explicitly cleared before validation to bypass E.164 validation
	clearPhone := false
	if req.Phone != nil && *req.Phone == "" {
		req.Phone = nil
		clearPhone = true
	}

	// 2. Validate parsed struct
	val := validator.New()
	if err := val.Validate(&req); err != nil {
		return err
	}

	// 3. Map validated request to updates map
	var updates = make(bson.M)
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.BusinessName != nil {
		updates["business_name"] = *req.BusinessName
	}
	if req.Address != nil {
		updates["address"] = *req.Address
	}
	if req.Phone != nil {
		updates["phone"] = *req.Phone
	} else if clearPhone {
		updates["phone"] = nil
	}
	if req.Slug != nil {
		updates["slug"] = *req.Slug
	}
	if req.ProfileImageURL != nil {
		if *req.ProfileImageURL == "" {
			updates["profile_image_url"] = nil
		} else {
			updates["profile_image_url"] = *req.ProfileImageURL
		}
	}
	if req.BannerImageURL != nil {
		if *req.BannerImageURL == "" {
			updates["banner_image_url"] = nil
		} else {
			updates["banner_image_url"] = *req.BannerImageURL
		}
	}
	if req.Settings != nil {
		settingsMap := make(bson.M)
		if req.Settings.DefaultQueueName != nil {
			settingsMap["default_queue_name"] = *req.Settings.DefaultQueueName
		}
		if req.Settings.AvgServiceMins != nil {
			settingsMap["avg_service_mins"] = *req.Settings.AvgServiceMins
		}
		if req.Settings.EmailNotifications != nil {
			settingsMap["email_notifications"] = *req.Settings.EmailNotifications
		}
		if req.Settings.PushNotifications != nil {
			settingsMap["push_notifications"] = *req.Settings.PushNotifications
		}
		updates["settings"] = settingsMap
	}

	const maxImageBytes = 1024 * 1024 // 1MB
	ctx := c.Context()

	supportedBrandingFields := []string{"banner_image_url", "profile_image_url", "business_name", "address"}
	hasBrandingUpdate := false

	for _, field := range supportedBrandingFields {
		if _, ok := updates[field]; ok {
			hasBrandingUpdate = true
			break
		}
	}

	if !hasBrandingUpdate {
		if _, err := c.FormFile("banner_image"); err == nil {
			hasBrandingUpdate = true
		} else if _, err := c.FormFile("profile_image"); err == nil {
			hasBrandingUpdate = true
		}
	}

	if hasBrandingUpdate {
		plan, err := h.billingService.GetHostPlan(ctx, hostID)
		if err != nil || plan == nil || !plan.Limits.CustomBranding {
			return fiber.NewError(fiber.StatusForbidden, "Custom branding is only available on Elite plans")
		}
	}

	imageFields := []struct {
		formName  string
		dbField   string
		typeLabel string
	}{
		{"profile_image", "profile_image_url", "profile"},
		{"banner_image", "banner_image_url", "banner"},
	}

	for _, field := range imageFields {
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
			val, _ := v.(string)
			if val == "" {
				updates[field.dbField] = nil
			} else {
				delete(updates, field.dbField)
			}
		}
	}

	if err := h.hostService.UpdateHost(ctx, hostID, updates); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
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
