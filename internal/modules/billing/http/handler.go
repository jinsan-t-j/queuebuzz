package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/helpers"
	"queuebuzz/internal/log"
	billingdto "queuebuzz/internal/modules/billing/dto"
	"queuebuzz/internal/modules/billing/provider"
	"queuebuzz/internal/modules/billing/service"

	"github.com/gofiber/fiber/v3"
	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Handler exposes HTTP endpoints for the billing module.
type Handler struct {
	cfg      *config.Config
	svc      *service.BillingService
	provider provider.PaymentProvider
	redis    *redisdriver.Client
}

// NewHandler constructs a billing HTTP handler.
func NewHandler(
	cfg *config.Config,
	svc *service.BillingService,
	provider provider.PaymentProvider,
	redis *redisdriver.Client,
) *Handler {
	return &Handler{
		cfg:      cfg,
		svc:      svc,
		provider: provider,
		redis:    redis,
	}
}

// resolveCountry extracts the visitor's country from headers.
func resolveCountry(c fiber.Ctx) string {
	if cfCountry := c.Get("CF-IPCountry"); cfCountry != "" && cfCountry != "XX" {
		return cfCountry
	}
	if tz := c.Get("X-Browser-Timezone"); tz != "" {
		if country := timezoneToCountry(tz); country != "" {
			return country
		}
	}
	if q := c.Query("country"); q != "" {
		return q
	}
	return "IN"
}

func timezoneToCountry(tz string) string {
	mapping := map[string]string{
		"Asia/Kolkata":        "IN",
		"Asia/Calcutta":       "IN",
		"Asia/Mumbai":         "IN",
		"Asia/Chennai":        "IN",
		"America/New_York":    "US",
		"America/Chicago":     "US",
		"America/Denver":      "US",
		"America/Los_Angeles": "US",
		"America/Phoenix":     "US",
		"Europe/London":       "GB",
		"Europe/Paris":        "FR",
		"Europe/Berlin":       "DE",
		"Asia/Dubai":          "AE",
		"Asia/Singapore":      "SG",
		"Australia/Sydney":    "AU",
		"Asia/Tokyo":          "JP",
		"Asia/Shanghai":       "CN",
		"Asia/Hong_Kong":      "HK",
		"Pacific/Auckland":    "NZ",
		"America/Toronto":     "CA",
		"America/Sao_Paulo":   "BR",
	}
	if country, ok := mapping[tz]; ok {
		return country
	}
	return ""
}

// ListPlans returns all available billing plans for the visitor's country.
//
// @Summary      List billing plans
// @Description  Returns all available plans (Free, Pro, Elite) localized to the visitor's
//
//	country. Country is resolved via CF-IPCountry header, X-Browser-Timezone
//	header, or explicit "country" query param, defaulting to "IN".
//	Results are cached per-country in Redis for 1 hour.
//
// @Tags         Billing
// @Produce      json
// @Param        country            query    string false "ISO 3166-1 alpha-2 country code (e.g. IN, US)"
// @Param        X-Browser-Timezone header   string false "IANA timezone (e.g. Asia/Kolkata) for auto country detection"
// @Param        CF-IPCountry       header   string false "Cloudflare edge-resolved country code"
// @Success      200 {object} helpers.SuccessResponse{data=billingdto.PlansResponse} "Localized plan list"
// @Failure      500 {object} helpers.ErrorResponse "Database or aggregation error"
// @Router       /api/v1/billing/plans [get]
func (h *Handler) ListPlans(c fiber.Ctx) error {
	country := resolveCountry(c)
	cacheKey := "billing:plans:" + country

	if h.redis != nil {
		if val, err := h.redis.Get(c.Context(), cacheKey).Result(); err == nil {
			return c.Status(fiber.StatusOK).SendString(val)
		}
	}

	plans, err := h.svc.ListPlans(c.Context(), country)
	if err != nil {
		return helpers.ErrorResponse{Message: "Failed to list plans"}.JSON(c, fiber.StatusInternalServerError)
	}

	res := helpers.NewSuccessResponse("", billingdto.PlansResponse{Plans: plans})

	if h.redis != nil {
		if jsonBytes, err := json.Marshal(res); err == nil {
			_ = h.redis.Set(c.Context(), cacheKey, jsonBytes, 1*time.Hour).Err()
		}
	}

	return res.OK(c)
}

// GetCheckoutURL creates a Dodo checkout session for a specific plan.
//
// @Summary      Create checkout session
// @Description  Authenticated hosts can create a checkout session for a paid plan.
//
//	Returns a secure payment URL from Dodo Payments. Free and enterprise plans
//	are rejected. Includes idempotency protection (30-second cooldown per host+plan).
//
// @Tags         Billing
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body billingdto.CheckoutRequest true "Plan selection"
// @Success      200 {object} helpers.SuccessResponse{data=billingdto.CheckoutResponse} "Contains 'url' field with redirect URL"
// @Failure      400 {object} helpers.ErrorResponse "Invalid plan, free plan, enterprise, missing product config"
// @Failure      401 {object} helpers.ErrorResponse "Host not authenticated"
// @Failure      429 {object} helpers.ErrorResponse "Duplicate checkout request within 30s window"
// @Router       /api/v1/billing/checkout [post]
func (h *Handler) GetCheckoutURL(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return helpers.ErrorResponse{Message: "Authentication required"}.JSON(c, fiber.StatusUnauthorized)
	}

	var req billingdto.CheckoutRequest
	if err := c.Bind().Body(&req); err != nil {
		return helpers.ErrorResponse{Message: "Invalid request body"}.JSON(c, fiber.StatusBadRequest)
	}

	if req.BillingCycle == "" {
		req.BillingCycle = "monthly"
	}

	idempotencyKey := "checkout:" + hostID + ":" + req.PlanID + ":" + req.BillingCycle
	res := h.redis.SetArgs(c.Context(), idempotencyKey, "1", redisdriver.SetArgs{
		Mode: "NX",
		TTL:  30 * time.Second,
	})
	if res.Err() != nil {
		return helpers.ErrorResponse{Message: "A checkout session is already being created."}.JSON(c, fiber.StatusTooManyRequests)
	}
	defer h.redis.Del(c.Context(), idempotencyKey)

	frontendOrigin := c.Get("Origin")
	if frontendOrigin == "" {
		frontendOrigin = h.cfg.FrontendURL
	}

	url, err := h.svc.CreateCheckoutURL(c.Context(), hostID, req.PlanID, req.BillingCycle, frontendOrigin)
	if err != nil {
		return helpers.ErrorResponse{Message: err.Error()}.JSON(c, fiber.StatusBadRequest)
	}

	return helpers.NewSuccessResponse("", billingdto.CheckoutResponse{URL: url}).OK(c)
}

// HandleWebhook processes inbound payment events from Dodo Payments.
//
// @Summary      Billing Webhook
// @Description  Endpoint for Dodo Payments to send subscription and payment events.
// @Tags         Billing
// @Accept       json
// @Produce      json
// @Param        webhook-id         header string true "Webhook ID"
// @Param        webhook-signature  header string true "Webhook Signature"
// @Param        webhook-timestamp  header string true "Webhook Timestamp"
// @Param        request body billingdto.WebhookRequest true "Webhook payload"
// @Success      200 {string} string "OK"
// @Failure      401 {string} string "Unauthorized"
// @Router       /api/v1/billing/webhook [post]
func (h *Handler) HandleWebhook(c fiber.Ctx) error {
	payload := c.Body()

	webhookID := c.Get("webhook-id")
	headers := map[string]string{
		"webhook-id":        webhookID,
		"webhook-signature": c.Get("webhook-signature"),
		"webhook-timestamp": c.Get("webhook-timestamp"),
	}

	event, err := h.provider.VerifyWebhook(payload, headers)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "Webhook signature verification failed: "+err.Error())
	}

	if event.HostID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Webhook event has no host_id in metadata")
	}

	// Idempotency: use webhook-id (guaranteed unique per delivery by Dodo)
	if webhookID != "" {
		idempotencyKey := "webhook:" + webhookID
		res := h.redis.SetArgs(c.Context(), idempotencyKey, "1", redisdriver.SetArgs{
			Mode: "NX",
			TTL:  72 * time.Hour,
		})
		if res.Err() != nil {
			log.Info().Str("webhook_id", webhookID).Msg("Duplicate webhook delivery, skipping")
			return c.SendStatus(fiber.StatusOK)
		}
	}

	if err := h.svc.ProcessBillingEvent(c.Context(), event); err != nil {
		log.Error().Err(err).Str("type", event.Type).Str("host_id", event.HostID).Msg("Failed to process billing event")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to process billing event: "+err.Error())
	}

	return c.SendStatus(fiber.StatusOK)
}

// GetCurrentPlan returns the subscription details for the logged-in host.
//
// @Summary      Get current plan
// @Description  Returns the active billing plan with feature limits, current usage counters,
//
//	and subscription status for the authenticated host. Falls back to the free
//	plan when no active subscription exists. Plan data is cached per-host in Redis.
//
// @Tags         Billing
// @Produce      json
// @Success      200 {object} helpers.SuccessResponse{data=billingdto.PlanResponse}
// @Router       /api/v1/billing/current-plan [get]
func (h *Handler) GetCurrentPlan(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		plan, err := h.svc.GetStandardFreePlan(c.Context())
		if err != nil {
			return helpers.ErrorResponse{Message: "Failed to get default free plan"}.JSON(c, fiber.StatusInternalServerError)
		}
		return helpers.NewSuccessResponse("", billingdto.PlanResponse{Plan: plan}).OK(c)
	}

	plan, err := h.svc.GetHostPlan(c.Context(), hostID)
	if err != nil {
		return helpers.ErrorResponse{Message: "Failed to get current plan"}.JSON(c, fiber.StatusInternalServerError)
	}

	return helpers.NewSuccessResponse("", billingdto.PlanResponse{Plan: plan}).OK(c)
}

// PaymentSuccess handles the redirect from Dodo Payments after a successful checkout.
// It redirects the user back to the frontend dashboard.
func (h *Handler) PaymentSuccess(c fiber.Ctx) error {
	origin := c.Query("origin")
	plan := c.Query("plan")
	if origin == "" {
		origin = h.cfg.FrontendURL
	}

	// Redirect to frontend dashboard with success flag
	// Path should match frontend route (/dashboard)
	return c.Redirect().To(fmt.Sprintf("%s/dashboard?checkout=success&plan=%s", origin, plan))
}

// PaymentCancel handles the redirect from Dodo Payments after a user cancels checkout.
// It redirects the user back to the frontend pricing page.
func (h *Handler) PaymentCancel(c fiber.Ctx) error {
	origin := c.Query("origin")
	if origin == "" {
		origin = h.cfg.FrontendURL
	}

	// Redirect back to pricing
	return c.Redirect().To(fmt.Sprintf("%s/pricing?checkout=error", origin))
}

// GetSubscription returns the host's subscription details.
//
// @Summary      Get subscription
// @Description  Returns the host's active subscription details including cancellation status.
// @Tags         Billing
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} helpers.SuccessResponse{data=billingdto.SubscriptionResponse}
// @Failure      401 {object} helpers.ErrorResponse
// @Failure      404 {object} helpers.ErrorResponse
// @Router       /api/v1/billing/subscription [get]
func (h *Handler) GetSubscription(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return helpers.ErrorResponse{Message: "Authentication required"}.JSON(c, fiber.StatusUnauthorized)
	}

	sub, err := h.svc.GetSubscription(c.Context(), hostID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return helpers.ErrorResponse{Message: "No active subscription found"}.JSON(c, fiber.StatusNotFound)
		}
		return helpers.ErrorResponse{Message: "Failed to fetch subscription"}.JSON(c, fiber.StatusInternalServerError)
	}

	// If card info is missing, trigger a background sync to try and recover it
	if sub.CardLast4 == "" && sub.ProviderSubscriptionID != "" {
		_ = h.svc.SyncSubscriptionDetails(c.Context(), sub)
	}

	// Resolve plan name and tier for the response
	planName := "Free"
	planTier := "free"
	canBranding := false
	canExport := false
	canHistory := false
	allowGeoLock := false
	plan, err := h.svc.GetPlanByID(c.Context(), sub.PlanID)
	if err == nil && plan != nil {
		planName = plan.Name
		planTier = plan.Tier
		canBranding = plan.Limits.CustomBranding
		canExport = plan.Limits.CanExport
		canHistory = plan.Limits.HistoryAccess
		allowGeoLock = plan.Limits.AllowGeoLock
	} else if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Failed to resolve plan details for active subscription")
	}

	resp := billingdto.SubscriptionResponse{
		PlanName:          planName,
		Tier:              planTier,
		Status:            string(sub.Status),
		BillingCycle:      sub.BillingCycle,
		CurrentPeriodEnd:  sub.CurrentPeriodEnd,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		CardLast4:         sub.CardLast4,
		CanCustomBranding: canBranding,
		CanExportData:     canExport,
		CanViewHistory:    canHistory,
		AllowGeoLock:      allowGeoLock,
		UpdatedAt:         sub.UpdatedAt,
	}

	return helpers.NewSuccessResponse("", resp).OK(c)
}

// CancelSubscription cancels the host's subscription at the end of the billing period.
//
// @Summary      Cancel subscription
// @Description  Schedules the host's subscription for cancellation at the end of the current billing period.
// @Tags         Billing
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body billingdto.CancelSubscriptionRequest true "Cancellation details"
// @Success      200 {object} helpers.SuccessResponse
// @Failure      400 {object} helpers.ErrorResponse
// @Failure      401 {object} helpers.ErrorResponse
// @Router       /api/v1/billing/subscription/cancel [post]
func (h *Handler) CancelSubscription(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return helpers.ErrorResponse{Message: "Authentication required"}.JSON(c, fiber.StatusUnauthorized)
	}

	var req billingdto.CancelSubscriptionRequest
	if err := c.Bind().Body(&req); err != nil {
		return helpers.ErrorResponse{Message: "Invalid request body"}.JSON(c, fiber.StatusBadRequest)
	}

	if err := h.svc.CancelSubscriptionAtPeriodEnd(c.Context(), hostID, req.Comment, req.Feedback); err != nil {
		return helpers.ErrorResponse{Message: err.Error()}.JSON(c, fiber.StatusBadRequest)
	}

	return helpers.NewSuccessResponse("Subscription will be cancelled at end of billing period", nil).OK(c)
}

// UpdatePaymentMethod returns a Dodo-hosted link for updating the payment method.
//
// @Summary      Update payment method
// @Description  Returns a secure payment link where the host can update their payment method.
// @Tags         Billing
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body billingdto.UpdatePaymentMethodRequest true "Return URL"
// @Success      200 {object} helpers.SuccessResponse
// @Failure      400 {object} helpers.ErrorResponse
// @Failure      401 {object} helpers.ErrorResponse
// @Router       /api/v1/billing/subscription/update-payment-method [post]
func (h *Handler) UpdatePaymentMethod(c fiber.Ctx) error {
	hostID, _ := c.Locals("host_id").(string)
	if hostID == "" {
		return helpers.ErrorResponse{Message: "Authentication required"}.JSON(c, fiber.StatusUnauthorized)
	}

	var req billingdto.UpdatePaymentMethodRequest
	if err := c.Bind().Body(&req); err != nil {
		return helpers.ErrorResponse{Message: "Invalid request body"}.JSON(c, fiber.StatusBadRequest)
	}

	if req.ReturnURL == "" {
		req.ReturnURL = h.cfg.FrontendURL + "/settings"
	}

	link, err := h.svc.UpdatePaymentMethod(c.Context(), hostID, req.ReturnURL)
	if err != nil {
		return helpers.ErrorResponse{Message: err.Error()}.JSON(c, fiber.StatusBadRequest)
	}

	return helpers.NewSuccessResponse("", map[string]string{"payment_link": link}).OK(c)
}
