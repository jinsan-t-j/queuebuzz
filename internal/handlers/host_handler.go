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
	cfg              *config.Config
	authService      *services.AuthService
	magicLinkService *services.MagicLinkService
	otpService       *services.OTPService
	emailService     *services.EmailService
	redisService     *services.RedisService
	hostCol          *mongo.Collection
	hostService      *services.HOST_SERVICE
	queueService     *services.QueueService
}

func NewHostHandler(
	cfg *config.Config,
	authSvc *services.AuthService,
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
			cfg:              cfg,
			authService:      authSvc,
			magicLinkService: magicLinkSvc,
			otpService:       otpSvc,
			emailService:     emailSvc,
			redisService:     redisSvc,
			hostCol:          hostCol,
			hostService:      hostSvc,
			queueService:     queueSvc,
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

	// Phone / OTP path
	_, err := h.otpService.GenerateAndStoreOTP(c.Context(), *req.Phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	return helpers.MessageResponse(c, "OTP sent.")
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
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	c.Cookie(&fiber.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Expires:  accessExp,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
		Path:     "/",
	})

	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Expires:  refreshExp,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
		Path:     "/",
	})

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

	// Get anonymous owner token from cookie
	ownerCookie := c.Cookies("owner_token")
	if ownerCookie == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing owner token cookie")
	}

	// Parse anonymous JWT to get queue_id
	anonClaims, err := h.authService.VerifyToken(ownerCookie)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized)
	}

	if anonClaims.Role != constants.RoleAnonymousHost || anonClaims.QueueID == "" {
		return fiber.NewError(fiber.StatusForbidden)
	}

	// Verify anonymous ownership
	if err := h.authService.VerifyAnonymousOwnership(c.Context(), ownerCookie, anonClaims.QueueID); err != nil {
		return fiber.NewError(fiber.StatusForbidden)
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	// Fetch the registered host
	var host models.Host
	err = h.hostCol.FindOne(ctx, bson.M{"_id": registeredHostID}).Decode(&host)
	if err != nil {
		return fiber.NewError(fiber.StatusForbidden)
	}

	// Update queue ownership
	_, err = h.queueService.GetQueue(ctx, anonClaims.QueueID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound)
	}

	// We need the queue collection directly for the update
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

	// Delete old owner token from Redis
	_ = h.redisService.DeleteOwnerToken(ctx, anonClaims.QueueID)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Queue claimed successfully",
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

// --- helpers ---
