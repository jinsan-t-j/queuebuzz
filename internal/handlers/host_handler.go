package handlers

import (
	"context"
	"crypto/rand"
	"math/big"
	"sync"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/models"
	"queuebuzz/internal/requests"
	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	hostHandlerInstance *HostHandler
	hostHandlerOnce     sync.Once
)

type HostHandler struct {
	authService      *services.AuthService
	magicLinkService *services.MagicLinkService
	otpService       *services.OTPService
	emailService     *services.EmailService
	redisService     *services.RedisService
	hostCol          *mongo.Collection
	queueService     *services.QueueService
}

func NewHostHandler(
	authSvc *services.AuthService,
	magicLinkSvc *services.MagicLinkService,
	otpSvc *services.OTPService,
	emailSvc *services.EmailService,
	redisSvc *services.RedisService,
	hostCol *mongo.Collection,
	queueSvc *services.QueueService,
) *HostHandler {
	hostHandlerOnce.Do(func() {
		hostHandlerInstance = &HostHandler{
			authService:      authSvc,
			magicLinkService: magicLinkSvc,
			otpService:       otpSvc,
			emailService:     emailSvc,
			redisService:     redisSvc,
			hostCol:          hostCol,
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
// @Param request body requests.RegisterRequest true "Register Request"
// @Success 200 {object} map[string]string
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

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"message": "Magic link sent. Check your email.",
		})
	}

	// Phone / OTP path
	_, err := h.otpService.GenerateAndStoreOTP(c.Context(), *req.Phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// TODO: Send OTP via SMS (not implemented in MVP)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "OTP sent.",
	})
}

// Verify godoc
// @Summary Verify Magic Link or OTP
// @Description Validates the magic link token or OTP and issues a JWT pair for the host
// @Tags Host
// @Accept json
// @Produce json
// @Param request body requests.VerifyRequest true "Verify Request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string "Error response"
// @Failure 401 {object} map[string]string "Error response"
// @Failure 500 {object} map[string]string "Error response"
// @Router /host/verify [post]
func (h *HostHandler) Verify(c fiber.Ctx) error {
	var req requests.VerifyRequest
	if err := c.Bind().JSON(&req); err != nil {
		return err
	}

	var email string
	var phone string

	if req.Token != nil {
		// Magic link path
		e, err := h.magicLinkService.VerifyMagicLink(c.Context(), *req.Token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized)
		}
		email = e
	} else if req.Phone != nil && req.OTP != nil {
		// OTP path
		if err := h.otpService.VerifyOTP(c.Context(), *req.Phone, *req.OTP); err != nil {
			return fiber.NewError(fiber.StatusUnauthorized)
		}
		phone = *req.Phone
	} else {
		return fiber.NewError(fiber.StatusBadRequest, "provide token or phone+otp")
	}

	// Create or fetch Host
	host, err := h.findOrCreateHost(c.Context(), email, phone)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// Issue JWT pair
	accessToken, refreshToken, accessExp, refreshExp, err := h.authService.IssueTokenPair(c.Context(), host.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError)
	}

	// Set refresh token as httpOnly cookie
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Expires:  refreshExp,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
		Path:     "/",
	})

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"access_token":   accessToken,
		"expires_at":     accessExp.Format(time.RFC3339),
		"token_type":     "Bearer",
		"host_id":        host.ID,
		"host_public_id": host.PublicID,
	})
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

func (h *HostHandler) findOrCreateHost(ctx context.Context, email, phone string) (*models.Host, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if email != "" {
		filter["email"] = email
	} else if phone != "" {
		filter["phone"] = phone
	}

	var host models.Host
	err := h.hostCol.FindOne(ctx, filter).Decode(&host)
	if err == nil {
		// Update last_seen
		_, _ = h.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
		return &host, nil
	}

	// Create new host
	now := time.Now()
	host = models.Host{
		ID:        generateHostID(),
		PublicID:  generateSlug(),
		Tier:      constants.TierFree,
		CreatedAt: now,
		LastSeen:  now,
	}
	if email != "" {
		host.Email = &email
	}
	if phone != "" {
		host.Phone = &phone
	}

	_, err = h.hostCol.InsertOne(ctx, host)
	if err != nil {
		return nil, err
	}

	return &host, nil
}

func generateHostID() string {
	return "host_" + uuid.New().String()[:8]
}

func generateSlug() string {
	adjectives := []string{"swift", "bright", "calm", "bold", "cool", "fast", "keen", "neat", "warm", "wise"}
	nouns := []string{"queue", "spot", "line", "desk", "gate", "lane", "zone", "hub", "dock", "pass"}

	adj := adjectives[randInt(len(adjectives))]
	noun := nouns[randInt(len(nouns))]
	num := 1000 + randInt(9000)

	return adj + "-" + noun + "-" + itoa(num)
}

func randInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

func itoa(n int) string {
	s := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		s[i] = '0' + byte(n%10)
		n /= 10
	}
	return string(s)
}
