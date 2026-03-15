package http

import (
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
	authService       *authservice.AuthService
	socialAuthService *authservice.SocialAuthService
	magicLinkService  *legacyservices.MagicLinkService
	otpService        *legacyservices.OTPService
	emailService      *legacyservices.EmailService
	hostService       *hostservice.Service
}

func NewHandler(
	cfg *config.Config,
	authSvc *authservice.AuthService,
	socialAuthSvc *authservice.SocialAuthService,
	magicLinkSvc *legacyservices.MagicLinkService,
	otpSvc *legacyservices.OTPService,
	emailSvc *legacyservices.EmailService,
	hostSvc *hostservice.Service,
) *Handler {
	return &Handler{
		cfg:               cfg,
		authService:       authSvc,
		socialAuthService: socialAuthSvc,
		magicLinkService:  magicLinkSvc,
		otpService:        otpSvc,
		emailService:      emailSvc,
		hostService:       hostSvc,
	}
}

func (h *Handler) Register(c fiber.Ctx) error {
	var req authdto.RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}
	if req.Email == nil && req.Phone == nil {
		return fiber.NewError(fiber.StatusBadRequest, "email or phone required")
	}
	if req.Email != nil {
		token, err := h.magicLinkService.GenerateAndStoreMagicLink(c.Context(), *req.Email)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError)
		}
		if err := h.emailService.SendMagicLink(*req.Email, token); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError)
		}
		return helpers.MessageResponse(c, "Magic link sent. Check your email.")
	}
	if _, err := h.otpService.GenerateAndStoreOTP(c.Context(), *req.Phone); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}
	return helpers.MessageResponse(c, "OTP sent.")
}

func (h *Handler) SocialLogin(c fiber.Ctx) error {
	url, err := h.socialAuthService.StartAuth(c.Context(), c.Params("provider"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Redirect().To(url)
}

func (h *Handler) SocialCallback(c fiber.Ctx) error {
	provider := c.Params("provider")
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
	host, err := h.hostService.FindOrCreateHostBySocial(c.Context(), identity)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.setAuthCookies(c, accessToken, refreshToken, accessExp, refreshExp)
	return c.Redirect().To(h.cfg.AuthCallbackURL)
}

func (h *Handler) Verify(c fiber.Ctx) error {
	var email string
	var phone string
	token := c.Query("token")
	reqPhone := c.Query("phone")
	otp := c.Query("otp")

	if token != "" {
		e, err := h.magicLinkService.VerifyMagicLink(c.Context(), token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized)
		}
		email = e
	} else if reqPhone != "" && otp != "" {
		if err := h.otpService.VerifyOTP(c.Context(), reqPhone, otp); err != nil {
			return fiber.NewError(fiber.StatusUnauthorized)
		}
		phone = reqPhone
	} else {
		return fiber.NewError(fiber.StatusBadRequest, "provide token or phone+otp")
	}

	host, err := h.hostService.FindOrCreateHost(c.Context(), email, phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.setAuthCookies(c, accessToken, refreshToken, accessExp, refreshExp)
	return c.Redirect().To(h.cfg.AuthCallbackURL)
}

func (h *Handler) setAuthCookies(c fiber.Ctx, accessToken, refreshToken string, accessExp, refreshExp time.Time) {
	c.Cookie(&fiber.Cookie{Name: "access_token", Value: accessToken, Expires: accessExp, HTTPOnly: true, Secure: h.cfg.IsProduction(), SameSite: "Lax", Path: "/"})
	c.Cookie(&fiber.Cookie{Name: "refresh_token", Value: refreshToken, Expires: refreshExp, HTTPOnly: true, Secure: h.cfg.IsProduction(), SameSite: "Lax", Path: "/"})
}
