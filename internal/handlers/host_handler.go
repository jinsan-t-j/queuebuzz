package handlers

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	"queuebuzz/internal/models"
	"queuebuzz/internal/requests"
	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	hostHandlerInstance *HostHandler
	hostHandlerOnce     sync.Once
)

type HostHandler struct {
	cfg               *config.Config
	authService       *services.AuthService
	socialAuthService *services.SocialAuthService
	magicLinkService  *services.MagicLinkService
	otpService        *services.OTPService
	emailService      *services.EmailService
	redisService      *services.RedisService
	hostCol           *mongo.Collection
	hostService       *services.HOST_SERVICE
	queueService      *services.QueueService
}

func NewHostHandler(
	cfg *config.Config,
	authSvc *services.AuthService,
	socialAuthSvc *services.SocialAuthService,
	magicLinkSvc *services.MagicLinkService,
	otpSvc *services.OTPService,
	emailSvc *services.EmailService,
	redisSvc *services.RedisService,
	hostCol *mongo.Collection,
	hostSvc *services.HOST_SERVICE,
	queueSvc *services.QueueService,
) *HostHandler {
	hostHandlerOnce.Do(func() {
		hostHandlerInstance = &HostHandler{
			cfg:               cfg,
			authService:       authSvc,
			socialAuthService: socialAuthSvc,
			magicLinkService:  magicLinkSvc,
			otpService:        otpSvc,
			emailService:      emailSvc,
			redisService:      redisSvc,
			hostCol:           hostCol,
			hostService:       hostSvc,
			queueService:      queueSvc,
		}
	})

	return hostHandlerInstance
}

// Register godoc
// @Summary Register a Host
// @Description Triggers Magic Link (email) or OTP (phone) for host registration
// @Tags Host
// @Accept json
// @Produce json
// @Param request body requests.RegisterRequest true "Magic link sent. Check your email."
// @Success 200 {object} responses.MessageResponse "Success response"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/register [post]
func (h *HostHandler) Register(c fiber.Ctx) error {
	var req requests.RegisterRequest
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

	_, err := h.otpService.GenerateAndStoreOTP(c.Context(), *req.Phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return helpers.MessageResponse(c, "OTP sent.")
}

// SocialLogin godoc
// @Summary Start social sign in
// @Description Starts the provider OAuth authorization code flow and redirects the browser to the selected provider.
// @Tags Host
// @Produce html
// @Param provider path string true "Social provider"
// @Success 302 {string} string "Redirect to provider"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/social/{provider}/start [get]
func (h *HostHandler) SocialLogin(c fiber.Ctx) error {
	provider := c.Params("provider")
	authorizationURL, err := h.socialAuthService.StartAuth(c.Context(), provider)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.Redirect().To(authorizationURL)
}

// SocialCallback godoc
// @Summary Complete social sign in
// @Description Handles OAuth provider callbacks, creates or links a host account, sets auth cookies, and redirects to the dashboard.
// @Tags Host
// @Produce html
// @Param provider path string true "Social provider"
// @Param code query string false "OAuth code"
// @Param state query string false "OAuth state"
// @Success 302 {string} string "Redirect to dashboard"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/social/{provider}/callback [get]
func (h *HostHandler) SocialCallback(c fiber.Ctx) error {
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

// Verify godoc
// @Summary Verify Magic Link or OTP
// @Description Validates the magic link token or OTP and issues a JWT pair for the host
// @Tags Host
// @Produce json
// @Param token query string false "Magic link token"
// @Param phone query string false "Phone number"
// @Param otp query string false "OTP code"
// @Success 200 redirect "Redirect to auth callback URL"
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /auth/verify [get]
func (h *HostHandler) Verify(c fiber.Ctx) error {
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

// Claim godoc
// @Summary Claim an anonymous queue
// @Description Allows an anonymous host to claim their queue as a registered host
// @Tags Host
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 403 {object} map[string]string "Error response"
// @Failure 404 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/claim [post]
// @Security BearerAuth
func (h *HostHandler) Claim(c fiber.Ctx) error {
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

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var host models.Host
	err = h.hostCol.FindOne(ctx, bson.M{"_id": registeredHostID}).Decode(&host)
	if err != nil {
		return fiber.NewError(fiber.StatusForbidden)
	}

	_, err = h.queueService.GetQueue(ctx, anonClaims.QueueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}

	queueCol := h.hostCol.Database().Collection("queues")
	_, err = queueCol.UpdateOne(ctx,
		bson.M{"_id": anonClaims.QueueID},
		bson.M{"$set": bson.M{
			"host_id":        registeredHostID,
			"host_public_id": host.PublicID,
		}},
	)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	_ = h.redisService.DeleteOwnerToken(ctx, anonClaims.QueueID)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Queue claimed successfully",
	})
}

// Logout godoc
// @Summary Logout host
// @Description Clears host auth cookies and revokes the refresh token if present
// @Tags Host
// @Produce json
// @Success 200 {object} map[string]string
// @Router /host/logout [post]
func (h *HostHandler) Logout(c fiber.Ctx) error {
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

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Signed out successfully",
	})
}

// GetMe godoc
// @Summary Get authenticated host
// @Description Returns the currently authenticated host profile based on the access token cookie or bearer token
// @Tags Host
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "Error response"
// @Failure 404 {object} map[string]string "Error response"
// @Router /host/me [get]
// @Security BearerAuth
func (h *HostHandler) GetMe(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var host models.Host
	err := h.hostCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host)
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

// GetProfile godoc
// @Summary Get host profile
// @Description Returns the host's public profile based on public_id
// @Tags Host
// @Accept json
// @Produce json
// @Param public_id path string true "Host Public ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string "Error response"
// @Router /host/{public_id} [get]
func (h *HostHandler) GetProfile(c fiber.Ctx) error {
	publicID := c.Params("public_id")

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var host models.Host
	err := h.hostCol.FindOne(ctx, bson.M{"public_id": publicID}).Decode(&host)
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

// GetQueues godoc
// @Summary Get active queues for host
// @Description Returns a list of the host's active queues based on public_id
// @Tags Host
// @Accept json
// @Produce json
// @Param public_id path string true "Host Public ID"
// @Success 200 {array} models.Queue
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/{public_id}/queues [get]
func (h *HostHandler) GetQueues(c fiber.Ctx) error {
	publicID := c.Params("public_id")

	queues, err := h.queueService.GetActiveQueuesForHost(c.Context(), publicID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(queues)
}

func (h *HostHandler) setAuthCookies(c fiber.Ctx, accessToken, refreshToken string, accessExp, refreshExp time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Expires:  accessExp,
		HTTPOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})

	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Expires:  refreshExp,
		HTTPOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})
}
