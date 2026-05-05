package http

import (
	"net/url"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/helpers"
	authdto "queuebuzz/internal/modules/auth/dto"
	authservice "queuebuzz/internal/modules/auth/service"
	hostservice "queuebuzz/internal/modules/host/service"
	legacyservices "queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	cfg               *config.Config
	redisService      *legacyservices.RedisService
	authService       *authservice.AuthService
	socialAuthService *authservice.SocialAuthService
	magicLinkService  *legacyservices.MagicLinkService
	otpService        *legacyservices.OTPService
	emailService      *legacyservices.EmailService
	hostService       *hostservice.Service
}

func NewHandler(
	cfg *config.Config,
	redisSvc *legacyservices.RedisService,
	authSvc *authservice.AuthService,
	socialAuthSvc *authservice.SocialAuthService,
	magicLinkSvc *legacyservices.MagicLinkService,
	otpSvc *legacyservices.OTPService,
	emailSvc *legacyservices.EmailService,
	hostSvc *hostservice.Service,
) *Handler {
	return &Handler{
		cfg:               cfg,
		redisService:      redisSvc,
		authService:       authSvc,
		socialAuthService: socialAuthSvc,
		magicLinkService:  magicLinkSvc,
		otpService:        otpSvc,
		emailService:      emailSvc,
		hostService:       hostSvc,
	}
}

// SocialLogin godoc
// @Summary Start social sign in
// @Description Starts the provider OAuth authorization code flow and redirects the browser to the selected provider.
// @Tags Auth
// @Produce html
// @Param provider path string true "Social provider"
// @Success 302 {string} string "Redirect to provider"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/social/{provider}/start [get]
func (h *Handler) SocialLogin(c fiber.Ctx) error {
	claimQueueID := c.Query("claim_queue_id")
	redirectURL := c.Query("redirect")
	url, err := h.socialAuthService.StartAuth(c.Params("provider"), "", claimQueueID, redirectURL)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Redirect().To(url)
}

// SocialCallback godoc
// @Summary Complete social sign in
// @Description Handles OAuth provider callbacks, creates or links a host account, sets auth cookies, and redirects to the dashboard.
// @Tags Auth
// @Produce html
// @Param provider path string true "Social provider"
// @Param code query string false "OAuth code"
// @Param state query string false "OAuth state"
// @Success 302 {string} string "Redirect to dashboard"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/social/{provider}/callback [get]
func (h *Handler) SocialCallback(c fiber.Ctx) error {
	provider := c.Params("provider")

	providerErr := c.Query("error")
	if providerErr == "" {
		providerErr = c.FormValue("error")
	}
	if providerErr != "" {
		description := c.Query("error_description")
		if description == "" {
			description = c.FormValue("error_description")
		}
		if description == "" {
			description = providerErr
		}

		u, _ := url.Parse(h.cfg.AuthCallbackURL)
		u.Path = "/error"
		u.RawQuery = url.Values{
			"title":       {"Authentication Error"},
			"error":       {providerErr},
			"description": {description},
			"action_text": {"Try Login Again"},
		}.Encode()
		return c.Redirect().To(u.String())
	}

	code := c.Query("code")
	state := c.Query("state")
	if code == "" {
		code = c.FormValue("code")
	}
	if state == "" {
		state = c.FormValue("state")
	}
	if code == "" || state == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing social auth callback parameters")
	}

	identity, err := h.socialAuthService.CompleteAuth(c.Context(), provider, code, state)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	host, created, err := h.hostService.FindOrCreateHostBySocial(c.Context(), identity)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if created && host.Email != nil {
		go h.emailService.SendWelcomeEmail(*host.Email, host.Name)
	}
	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID, host.PublicID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.setAuthCookies(c, accessToken, refreshToken, accessExp, refreshExp)

	targetURL := h.cfg.AuthCallbackURL
	if identity.ClaimQueueID != "" || identity.RedirectURL != "" {
		u, _ := url.Parse(targetURL)
		q := u.Query()
		if identity.ClaimQueueID != "" {
			q.Set("claim_queue_id", identity.ClaimQueueID)
		}
		if identity.RedirectURL != "" {
			q.Set("redirect", identity.RedirectURL)
		}
		u.RawQuery = q.Encode()
		targetURL = u.String()
	}
	return c.Redirect().To(targetURL)
}

// Verify godoc
// @Summary Verify magic link or OTP
// @Description Verifies a magic link or OTP, creates or links a host account, sets auth cookies, and redirects to the dashboard.
// @Tags Auth
// @Produce html
// @Param token query string false "Magic link token"
// @Param phone query string false "Phone number"
// @Param otp query string false "OTP code"
// @Success 302 {string} string "Redirect to dashboard"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/verify [get]
func (h *Handler) Verify(c fiber.Ctx) error {
	var email string
	var phone string
	var claimQueueID string
	var redirectURL string
	token := c.Query("token")
	reqPhone := c.Query("phone")
	otp := c.Query("otp")

	if token != "" {
		payload, err := h.magicLinkService.VerifyMagicLink(c.Context(), token)
		if err != nil {
			u, _ := url.Parse(h.cfg.AuthCallbackURL)
			u.Path = "/error"
			u.RawQuery = url.Values{
				"title":       {"Session Error"},
				"error":       {"invalid_token"},
				"description": {"The magic link is invalid or has expired."},
				"action_text": {"Back to Login"},
			}.Encode()
			return c.Redirect().To(u.String())
		}
		email = payload.Email
		claimQueueID = payload.ClaimQueueID
		redirectURL = payload.RedirectURL
	} else if reqPhone != "" && otp != "" {
		if err := h.otpService.VerifyOTP(c.Context(), reqPhone, otp); err != nil {
			u, _ := url.Parse(h.cfg.AuthCallbackURL)
			u.Path = "/error"
			u.RawQuery = url.Values{
				"title":       {"Verification Error"},
				"error":       {"invalid_otp"},
				"description": {"The OTP code you entered is incorrect or has expired."},
				"action_text": {"Back to Login"},
			}.Encode()
			return c.Redirect().To(u.String())
		}
		phone = reqPhone
	} else {
		return fiber.NewError(fiber.StatusBadRequest, "provide token or phone+otp")
	}

	host, created, err := h.hostService.FindOrCreateHost(c.Context(), email, phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if created && host.Email != nil {
		go h.emailService.SendWelcomeEmail(*host.Email, host.Name)
	}
	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID, host.PublicID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.setAuthCookies(c, accessToken, refreshToken, accessExp, refreshExp)

	targetURL := h.cfg.AuthCallbackURL
	if claimQueueID != "" || redirectURL != "" {
		u, _ := url.Parse(targetURL)
		q := u.Query()
		if claimQueueID != "" {
			q.Set("claim_queue_id", claimQueueID)
		}
		if redirectURL != "" {
			q.Set("redirect", redirectURL)
		}
		u.RawQuery = q.Encode()
		targetURL = u.String()
	}
	return c.Redirect().To(targetURL)
}

// Refresh godoc
// @Summary Refresh access token
// @Description Uses the refresh token cookie to issue a new access token and refresh token
// @Tags Auth
// @Produce json
// @Success 200 {object} helpers.SuccessResponse
// @Failure 401 {object} map[string]string "Error response"
// @Router /api/v1/auth/refresh [post]
func (h *Handler) Refresh(c fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "missing refresh token")
	}

	accessToken, newRefreshToken, accessExp, refreshExp, err := h.authService.RefreshTokenPair(c.Context(), refreshToken)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired refresh token")
	}

	h.setAuthCookies(c, accessToken, newRefreshToken, accessExp, refreshExp)
	return helpers.NewSuccessResponse("Token refreshed successfully", nil).MessageResponse(c)
}

// Logout godoc
// @Summary Logout host
// @Description Clears host auth cookies and revokes the refresh token if present
// @Tags Auth
// @Produce json
// @Success 200 {object} helpers.SuccessResponse
// @Router /api/v1/auth/logout [post]
func (h *Handler) Logout(c fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken != "" {
		_ = h.redisService.DeleteRefreshToken(c.Context(), refreshToken)
	}

	for _, name := range []string{"access_token", "refresh_token", "queuebuzz_host_token"} {
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
	return helpers.NewSuccessResponse("Signed out successfully", nil).MessageResponse(c)
}

// Authenticate godoc
// @Summary Login or Register via email
// @Description Handles authentication entry point. If the email is social-linked, redirects to provider. If not, sends a magic link.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body authdto.CheckMethodRequest true "Authentication request"
// @Success 200 {object} helpers.SuccessResponse "Magic link sent"
// @Success 302 {string} string "Redirect to social provider"
// @Failure 400 {object} map[string]string "Invalid request"
// @Router /api/v1/auth/login [post]
func (h *Handler) Authenticate(c fiber.Ctx) error {
	var req authdto.CheckMethodRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	if err := h.emailService.ValidateEmail(req.Email); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	method, provider, err := h.hostService.CheckAuthMethod(c.Context(), req.Email)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if method == "social" && provider != "" {
		url, err := h.socialAuthService.StartAuth(provider, req.Email, req.ClaimQueueID, req.RedirectURL)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return helpers.NewSuccessResponse("Social login", fiber.Map{
			"method":       method,
			"provider":     provider,
			"redirect_url": url,
		}).OK(c)
	}

	// For any other case (magic-link or no account), initiate magic link dispatching
	token, err := h.magicLinkService.GenerateAndStoreMagicLink(c.Context(), req.Email, req.ClaimQueueID, req.RedirectURL)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if err := h.emailService.SendMagicLink(req.Email, token); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return helpers.NewSuccessResponse("Magic link sent. Check your email.", nil).MessageResponse(c)
}

func (h *Handler) setAuthCookies(c fiber.Ctx, accessToken, refreshToken string, accessExp, refreshExp time.Time) {
	c.Cookie(&fiber.Cookie{Name: "access_token", Value: accessToken, Expires: accessExp, HTTPOnly: true, Secure: h.cfg.IsProduction(), SameSite: "Lax", Path: "/"})
	c.Cookie(&fiber.Cookie{Name: "refresh_token", Value: refreshToken, Expires: refreshExp, HTTPOnly: true, Secure: h.cfg.IsProduction(), SameSite: "Lax", Path: "/"})
}

// RefreshEmailBlocklist godoc
// @Summary Manually refresh the disposable email blocklist
// @Description Fetches the latest disposable email domains from the source GitHub repository and updates the in-memory map.
// @Tags System
// @Success 200 {object} map[string]string "Refresh successful"
// @Failure 500 {object} map[string]string "Error response"
// @Router /system/email/refresh-blocklist [post]
func (h *Handler) RefreshEmailBlocklist(c fiber.Ctx) error {
	// Simple Bearer token check
	authHeader := c.Get("Authorization")
	expectedToken := "Bearer " + h.cfg.SystemAPISecret

	// If secret is not set in config, we allow it in dev, but require it in production
	if h.cfg.IsProduction() && (h.cfg.SystemAPISecret == "" || authHeader != expectedToken) {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized system access")
	}

	if err := h.emailService.RefreshBlocklist(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to refresh blocklist: "+err.Error())
	}
	return c.JSON(fiber.Map{"message": "email blocklist refreshed successfully"})
}
