package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/billing/domain"
	"queuebuzz/internal/modules/billing/provider"
	hostdomain "queuebuzz/internal/modules/host/domain"
	systemservice "queuebuzz/internal/modules/system/service"
	"queuebuzz/internal/services"

	"github.com/google/uuid"
	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type BillingService struct {
	plansCol   *mongodriver.Collection
	subsCol    *mongodriver.Collection
	txsCol     *mongodriver.Collection
	recordsCol *mongodriver.Collection
	hostsCol   *mongodriver.Collection
	redis      *redisdriver.Client

	systemSvc *systemservice.SystemService
	emailSvc  *services.EmailService
	payment   provider.PaymentProvider
}

func NewBillingService(
	db *mongodriver.Database,
	redis *redisdriver.Client,
	systemSvc *systemservice.SystemService,
	emailSvc *services.EmailService,
	payment provider.PaymentProvider,
) *BillingService {
	return &BillingService{
		plansCol:   db.Collection("billing_plans"),
		subsCol:    db.Collection("billing_subscriptions"),
		txsCol:     db.Collection("billing_transactions"),
		recordsCol: db.Collection("billing_records"),
		hostsCol:   db.Collection("hosts"),
		redis:      redis,
		systemSvc:  systemSvc,
		emailSvc:   emailSvc,
		payment:    payment,
	}
}

func (s *BillingService) GetHostPlan(ctx context.Context, hostID string) (*domain.Plan, error) {
	if cached := s.getCachedHostPlan(ctx, hostID); cached != nil {
		return cached, nil
	}

	sub, err := s.GetSubscription(ctx, hostID)
	if err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return s.handleNoSubscription(ctx, hostID)
		}
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	planID := sub.PlanID

	plan, err := s.GetPlanByID(ctx, planID)
	if err == nil {
		s.setCacheHostPlan(ctx, hostID, plan, 1*time.Hour)
	}
	return plan, err
}

func (s *BillingService) downgradeHostTier(ctx context.Context, hostID string) error {
	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{
		"$set": bson.M{"tier": "free"},
	})
	return err
}

func (s *BillingService) handleNoSubscription(ctx context.Context, hostID string) (*domain.Plan, error) {
	p, err := s.getStandardFreePlan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch default plan: %w", err)
	}
	s.setCacheHostPlan(ctx, hostID, p, 10*time.Minute)
	return p, nil
}

func (s *BillingService) GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error) {
	var plan domain.Plan
	filter := bson.M{"_id": planID}
	if oid, err := bson.ObjectIDFromHex(planID); err == nil {
		filter = bson.M{"_id": bson.M{"$in": []any{planID, oid}}}
	}

	err := s.plansCol.FindOne(ctx, filter).Decode(&plan)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan %s: %w", planID, err)
	}
	return &plan, nil
}

func (s *BillingService) IsLimitExceeded(ctx context.Context, hostID string, limitType string, currentVal int) (bool, error) {
	plan, err := s.GetHostPlan(ctx, hostID)
	if err != nil {
		return false, err
	}

	switch limitType {
	case "queues_monthly":
		if plan.Limits.MaxQueuesPerMonth <= 0 {
			return false, nil
		}
		usage := currentVal
		if usage == -1 {
			var host hostdomain.Host
			if err := s.hostsCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host); err == nil {
				// Self-healing: if it's been more than a month since last reset, do it now
				// This handles cases where the webhook might have been missed or failed
				if host.MonthlyLimitResetAt.IsZero() || time.Since(host.MonthlyLimitResetAt) > 31*24*time.Hour {
					_ = s.ResetMonthlyLimits(ctx, hostID)
					usage = 0
				} else {
					usage = host.MonthlyQueueCount
				}
			}
		}
		return usage >= plan.Limits.MaxQueuesPerMonth, nil
	case "guests":
		if plan.Limits.MaxGuestsPerQueue <= 0 {
			return false, nil
		}
		return currentVal > plan.Limits.MaxGuestsPerQueue, nil
	default:
		return false, nil
	}
}

func (s *BillingService) CheckHistoryAccess(ctx context.Context, hostID string) (bool, error) {
	plan, err := s.GetHostPlan(ctx, hostID)
	if err != nil {
		return false, err
	}
	if plan == nil {
		return false, nil
	}
	return plan.Limits.HistoryAccess, nil
}

func (s *BillingService) CanViewGuestData(ctx context.Context, hostID string) (bool, error) {
	if hostID == "" {
		return false, nil // Anonymous hosts never see raw data
	}
	plan, err := s.GetHostPlan(ctx, hostID)
	if err != nil {
		return false, err
	}
	return plan.Limits.CanViewGuestData, nil
}

func (s *BillingService) GetHistoryRetentionDays(ctx context.Context, hostID string) (int, error) {
	plan, err := s.GetHostPlan(ctx, hostID)
	if err != nil {
		return 0, err
	}
	return plan.Limits.HistoryRetentionDays, nil
}

func (s *BillingService) ListPlans(ctx context.Context, country string) ([]domain.Plan, error) {
	matchCountries := []string{"GLOBAL", ""}
	if country != "" && country != "GLOBAL" {
		matchCountries = append(matchCountries, country)
	}

	pipeline := mongodriver.Pipeline{
		{{Key: "$match", Value: bson.M{
			"country_code": bson.M{"$in": matchCountries},
		}}},
		{{Key: "$addFields", Value: bson.M{
			"is_country": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$country_code", country}}, 1, 0}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "is_country", Value: -1},
			{Key: "created_at", Value: -1},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": "$tier",
			"doc": bson.M{"$first": "$$ROOT"},
		}}},
		{{Key: "$replaceRoot", Value: bson.M{"newRoot": "$doc"}}},
		{{Key: "$sort", Value: bson.M{"priority": 1}}}, // Sort by priority (Free -> Enterprise)
	}

	cursor, err := s.plansCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var filteredPlans []domain.Plan
	if err := cursor.All(ctx, &filteredPlans); err != nil {
		return nil, err
	}

	return filteredPlans, nil
}

// CreateCheckoutURL validates the request, creates a checkout session, and
// records a pending transaction for audit. Returns the redirect URL.
func (s *BillingService) CreateCheckoutURL(ctx context.Context, hostID, planID, billingCycle, frontendOrigin string) (string, error) {
	// 1. Validate billing cycle
	billingCycle = strings.ToLower(billingCycle)
	if billingCycle != "monthly" && billingCycle != "yearly" {
		return "", fmt.Errorf("invalid billing cycle: must be 'monthly' or 'yearly'")
	}

	// 2. Look up the host
	var host hostdomain.Host
	if err := s.hostsCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host); err != nil {
		return "", fmt.Errorf("host not found: %w", err)
	}

	// 3. Look up the plan
	plan, err := s.GetPlanByID(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("plan not found: %w", err)
	}

	// 4. Guard: free plans don't need checkout
	if plan.IsFree {
		return "", fmt.Errorf("cannot checkout a free plan")
	}

	// 5. Guard: enterprise plans require manual contact
	if plan.Tier == "enterprise" {
		return "", fmt.Errorf("enterprise plans require contacting sales")
	}

	// 6. Guard: check provider product IDs are configured
	productID := plan.ProviderMonthlyProductID
	if billingCycle == "yearly" {
		productID = plan.ProviderYearlyProductID
	}
	if productID == "" {
		return "", fmt.Errorf("payment not configured for this plan and billing cycle")
	}

	// 7. Guard: prevent duplicate active subscriptions for the same plan
	var existingSub domain.Subscription
	err = s.subsCol.FindOne(ctx, bson.M{
		"host_id":      hostID,
		"plan_id":      planID,
		"status":       domain.SubscriptionActive,
		"cancelled_at": nil,
	}).Decode(&existingSub)
	if err == nil {
		return "", fmt.Errorf("you already have an active subscription for this plan")
	}

	// 8. Build return URLs (pointing to our backend handlers)
	cfg := config.Get()
	backendBase := cfg.AppURL
	if before, ok := strings.CutSuffix(backendBase, "/"); ok {
		backendBase = before
	}

	successURL := fmt.Sprintf("%s/api/v1/billing/success?origin=%s&plan=%s", backendBase, frontendOrigin, plan.Slug)
	cancelURL := fmt.Sprintf("%s/api/v1/billing/cancel?origin=%s", backendBase, frontendOrigin)

	// 9. Create checkout session via provider
	resp, err := s.payment.CreateCheckoutSession(provider.CheckoutRequest{
		Host:         host,
		Plan:         *plan,
		BillingCycle: billingCycle,
		SuccessURL:   successURL,
		CancelURL:    cancelURL,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create checkout: %w", err)
	}

	// 10. Record pending transaction for audit trail
	tx := domain.Transaction{
		ID:                "tx_" + uuid.New().String()[:8],
		HostID:            hostID,
		PlanID:            planID,
		BillingCycle:      billingCycle,
		Amount:            0, // Amount comes from webhook after payment
		Currency:          plan.Currency,
		Status:            domain.TransactionPending,
		Provider:          "dodo",
		ProviderPaymentID: resp.SessionID,
		CreatedAt:         time.Now(),
	}
	if _, err := s.txsCol.InsertOne(ctx, tx); err != nil {
		log.Error().Err(err).Str("host_id", hostID).Msg("Failed to record pending transaction")
		// Non-fatal: checkout can still proceed
	}

	return resp.URL, nil
}

// HandleFailedPayment records a failed transaction and sets grace period.
func (s *BillingService) HandleFailedPayment(ctx context.Context, event *provider.BillingEvent) error {
	now := time.Now()

	tx := domain.Transaction{
		ID:                "tx_" + uuid.New().String()[:8],
		HostID:            event.HostID,
		PlanID:            event.PlanID,
		BillingCycle:      event.BillingCycle,
		Amount:            event.Amount,
		Currency:          event.Currency,
		Status:            domain.TransactionFailed,
		Provider:          "dodo",
		ProviderPaymentID: event.ProviderPaymentID,
		FailureReason:     event.Type,
		CardLast4:         event.CardLast4,
		CardBrand:         event.CardBrand,
		CardExpiry:        event.CardExpiry,
		CreatedAt:         now,
	}
	if _, err := s.txsCol.InsertOne(ctx, tx); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to record failed transaction")
	}

	graceDays := 3
	settings, _ := s.systemSvc.GetSettings(ctx)
	if settings != nil && settings.DefaultQueueSubscriptionGracePeriodDays > 0 {
		graceDays = settings.DefaultQueueSubscriptionGracePeriodDays
	}

	graceUntil := now.AddDate(0, 0, graceDays)
	update := bson.M{
		"status":             domain.SubscriptionInGrace,
		"grace_period_until": graceUntil,
		"updated_at":         now,
	}

	if event.CardLast4 != "" {
		update["card_last4"] = event.CardLast4
		update["card_brand"] = event.CardBrand
		update["card_expiry"] = event.CardExpiry
	}

	res, err := s.subsCol.UpdateOne(ctx, bson.M{"host_id": event.HostID}, bson.M{"$set": update})
	if err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to update subscription in HandleFailedPayment")
		return err
	}

	if res.MatchedCount == 0 {
		log.Warn().Str("host_id", event.HostID).Msg("No subscription found during HandleFailedPayment, skipping grace update")
		return nil
	}

	s.invalidateHostPlanCache(ctx, event.HostID)
	return nil
}

// HandleCancellation records the cancellation, downgrades the host to free tier,
// and marks the subscription as canceled.
func (s *BillingService) HandleCancellation(ctx context.Context, event *provider.BillingEvent) error {
	now := time.Now()

	// Record an audit transaction for the cancellation. We use TransactionFailed
	// with a "cancelled" reason to indicate this event didn't generate revenue.
	tx := domain.Transaction{
		ID:                "tx_" + uuid.New().String()[:8],
		HostID:            event.HostID,
		PlanID:            event.PlanID,
		BillingCycle:      event.BillingCycle,
		Amount:            0,
		Currency:          event.Currency,
		Status:            domain.TransactionFailed,
		Provider:          "dodo",
		ProviderPaymentID: event.ProviderPaymentID,
		FailureReason:     "cancelled",
		CreatedAt:         now,
	}
	if _, err := s.txsCol.InsertOne(ctx, tx); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to record cancellation transaction")
	}

	// Mark subscription as canceled
	_, err := s.subsCol.UpdateOne(ctx, bson.M{"host_id": event.HostID}, bson.M{
		"$set": bson.M{
			"status":       domain.SubscriptionCanceled,
			"cancelled_at": now,
			"updated_at":   now,
		},
	})
	if err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to cancel subscription")
	}

	// Downgrade host to free tier immediately
	if _, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": event.HostID}, bson.M{
		"$set": bson.M{"tier": "free"},
	}); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to downgrade host tier")
	}

	// Invalidate host plan cache so subsequent reads reflect the downgrade
	s.invalidateHostPlanCache(ctx, event.HostID)

	return nil
}

// HasExistingSubscription returns true if the host already has an active or
// in_grace subscription. Used to differentiate first purchase from renewal.
func (s *BillingService) HasExistingSubscription(ctx context.Context, hostID string) bool {
	var sub domain.Subscription
	err := s.subsCol.FindOne(ctx, bson.M{
		"host_id":      hostID,
		"status":       bson.M{"$in": []string{string(domain.SubscriptionActive), string(domain.SubscriptionInGrace)}},
		"cancelled_at": nil,
	}).Decode(&sub)
	return err == nil
}

// ResetMonthlyLimits clears the host's monthly queue usage counter on renewal.
// This ensures hosts get a fresh allocation each billing cycle.
func (s *BillingService) ResetMonthlyLimits(ctx context.Context, hostID string) error {
	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{
		"$set": bson.M{
			"monthly_queue_count":    0,
			"monthly_limit_reset_at": time.Now(),
		},
	})
	if err != nil {
		log.Error().Err(err).Str("host_id", hostID).Msg("Failed to reset monthly limits")
	}
	return err
}

// IncrementMonthlyQueueCount increments the host's monthly queue usage counter.
func (s *BillingService) IncrementMonthlyQueueCount(ctx context.Context, hostID string) error {
	if hostID == "" {
		return nil
	}
	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{
		"$inc": bson.M{
			"monthly_queue_count": 1,
			"total_queue_count":   1,
		},
	})
	if err != nil {
		log.Error().Err(err).Str("host_id", hostID).Msg("Failed to increment monthly queue count")
	}
	return err
}

// DecrementMonthlyQueueCount decrements the host's monthly queue usage counter if the queue was created
// in the current billing cycle. This allows hosts to "free up" a slot if they delete a queue.
func (s *BillingService) DecrementMonthlyQueueCount(ctx context.Context, hostID string, queueCreatedAt time.Time) error {
	if hostID == "" {
		return nil
	}

	var host hostdomain.Host
	if err := s.hostsCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host); err != nil {
		return err
	}

	inc := bson.M{}
	if host.TotalQueueCount > 0 {
		inc["total_queue_count"] = -1
	}

	// Only decrement monthly count if the queue was created after the last reset
	if host.MonthlyQueueCount > 0 && !queueCreatedAt.Before(host.MonthlyLimitResetAt) {
		inc["monthly_queue_count"] = -1
	}

	if len(inc) == 0 {
		return nil
	}

	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{"$inc": inc})
	if err != nil {
		log.Error().Err(err).Str("host_id", hostID).Msg("Failed to decrement monthly queue count")
	}
	return err
}

// DecrementMonthlyQueueCountBulk handles multiple queue deletions in a single host update.
func (s *BillingService) DecrementMonthlyQueueCountBulk(ctx context.Context, hostID string, queueCreatedAts []time.Time) error {
	if hostID == "" || len(queueCreatedAts) == 0 {
		return nil
	}

	var host hostdomain.Host
	if err := s.hostsCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host); err != nil {
		return err
	}

	monthlyDecr := 0
	for _, createdAt := range queueCreatedAts {
		if !createdAt.Before(host.MonthlyLimitResetAt) {
			monthlyDecr++
		}
	}

	inc := bson.M{}
	// Decrement total count (capped at current value)
	totalDecr := len(queueCreatedAts)
	if host.TotalQueueCount < totalDecr {
		totalDecr = host.TotalQueueCount
	}
	if totalDecr > 0 {
		inc["total_queue_count"] = -totalDecr
	}

	// Decrement monthly count (capped at current value)
	if monthlyDecr > 0 {
		if host.MonthlyQueueCount < monthlyDecr {
			monthlyDecr = host.MonthlyQueueCount
		}
		if monthlyDecr > 0 {
			inc["monthly_queue_count"] = -monthlyDecr
		}
	}

	if len(inc) == 0 {
		return nil
	}

	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{"$inc": inc})
	if err != nil {
		log.Error().Err(err).Str("host_id", hostID).Msg("Failed to bulk decrement queue counts")
	}
	return err
}

func (s *BillingService) IncrementTotalServedCount(ctx context.Context, hostID string) error {
	if hostID == "" {
		return nil
	}
	_, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{
		"$inc": bson.M{"total_served_count": 1},
	})
	return err
}

// ProcessBillingEvent handles a normalized billing event by routing it to the
// correct business logic handlers.
//
// Event routing strategy:
//   - payment.succeeded  → create Transaction + BillingRecord, upsert Subscription, upgrade host
//   - payment.failed     → create failed Transaction, set grace period
//   - subscription.active/renewed → update Subscription status + period (no transaction)
//   - subscription.updated       → sync cancellation metadata from provider
//   - subscription.cancelled     → mark Subscription cancelled, downgrade host
//   - subscription.failed        → set grace period on Subscription
func (s *BillingService) ProcessBillingEvent(ctx context.Context, event *provider.BillingEvent) error {
	hostEmail, _ := s.GetHostEmail(ctx, event.HostID)
	planName := s.GetHostPlanName(ctx, event.PlanID)

	// Invalidate host plan cache after any billing event
	s.invalidateHostPlanCache(ctx, event.HostID)

	log.Info().Str("type", event.Type).Str("host_id", event.HostID).Str("status", string(event.Status)).Msg("Processing billing event")

	hasExistingSub := s.HasExistingSubscription(ctx, event.HostID)

	switch event.Type {
	case domain.EventPaymentSucceeded:
		if err := s.HandlePaymentSucceeded(ctx, event); err != nil {
			return err
		}

		if hostEmail != "" && s.emailSvc != nil {
			if !hasExistingSub {
				_ = s.emailSvc.SendPurchaseConfirmation(hostEmail, planName, event.BillingCycle, event.TotalAmount, event.Currency)
			}
		}
		return nil

	case domain.EventPaymentFailed:
		err := s.HandleFailedPayment(ctx, event)
		if hostEmail != "" && s.emailSvc != nil {
			_ = s.emailSvc.SendPaymentFailed(hostEmail, planName, event.BillingCycle)
		}
		return err

	case domain.EventSubscriptionActive, domain.EventSubscriptionRenewed:
		// Dodo sometimes sends subscription.renewed on initial creation.
		// We only process real renewals (where there's a previous billing period).
		if event.Type == domain.EventSubscriptionRenewed && event.PreviousBillingDate == 0 {
			return nil
		}
		return s.HandleSubscriptionStateUpdate(ctx, event, hostEmail, planName)

	case domain.EventSubscriptionUpdated:
		// Sync cancellation metadata (cancel_at_period_end, feedback, etc.)
		return s.HandleSubscriptionUpdated(ctx, event)

	case domain.EventSubscriptionFailed:
		err := s.HandleFailedPayment(ctx, event)
		if hostEmail != "" && s.emailSvc != nil {
			_ = s.emailSvc.SendPaymentFailed(hostEmail, planName, event.BillingCycle)
		}
		return err

	case domain.EventSubscriptionCancelled:
		err := s.HandleCancellation(ctx, event)
		if hostEmail != "" && s.emailSvc != nil {
			_ = s.emailSvc.SendSubscriptionCancelled(hostEmail, planName)
		}
		return err

	case domain.EventSubscriptionPending:
		if hostEmail != "" && s.emailSvc != nil {
			_ = s.emailSvc.SendSubscriptionPending(hostEmail, planName)
		}
		// Also update DB state to pending
		return s.HandleSubscriptionStateUpdate(ctx, event, hostEmail, planName)

	default:
		log.Warn().Str("event_type", event.Type).Msg("Unprocessed billing event type")
		return nil
	}
}

// HandlePaymentSucceeded creates a Transaction + BillingRecord and upserts the Subscription.
// This is the ONLY path that creates financial records.
func (s *BillingService) HandlePaymentSucceeded(ctx context.Context, event *provider.BillingEvent) error {
	now := time.Now()

	txID := "tx_" + uuid.New().String()[:8]
	tx := domain.Transaction{
		ID:                 txID,
		HostID:             event.HostID,
		PlanID:             event.PlanID,
		BillingCycle:       event.BillingCycle,
		Amount:             event.Amount,
		TotalAmount:        event.TotalAmount,
		Currency:           event.Currency,
		Status:             domain.TransactionSucceeded,
		Provider:           "dodo",
		ProviderPaymentID:  event.ProviderPaymentID,
		ProviderCustomerID: event.ProviderCustomerID,
		InvoiceURL:         event.InvoiceURL,
		CardLast4:          event.CardLast4,
		CardBrand:          event.CardBrand,
		CardExpiry:         event.CardExpiry,
		CreatedAt:          now,
	}
	if _, err := s.txsCol.InsertOne(ctx, tx); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to record successful transaction")
	}

	plan, _ := s.GetPlanByID(ctx, event.PlanID)
	planName := ""
	if plan != nil {
		planName = plan.Name
	}

	record := domain.BillingRecord{
		ID:            "br_" + uuid.New().String()[:8],
		TransactionID: txID,
		HostID:        event.HostID,
		PlanID:        event.PlanID,
		PlanName:      planName,
		BillingCycle:  event.BillingCycle,
		Amount:        event.TotalAmount,
		Currency:      event.Currency,
		InvoiceNumber: generateInvoiceNumber(),
		BilledAt:      now,
	}
	if _, err := s.recordsCol.InsertOne(ctx, record); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to record billing record")
	}

	// 4. Upsert subscription (idempotent via host_id upsert)
	periodEnd := now.AddDate(0, 1, 0) // Default 1 month
	if event.BillingCycle == "yearly" {
		periodEnd = now.AddDate(1, 0, 0)
	}
	if event.CurrentPeriodEnd > 0 {
		periodEnd = time.Unix(event.CurrentPeriodEnd, 0)
	}

	subUpdate := bson.M{
		"host_id":                  event.HostID,
		"plan_id":                  event.PlanID,
		"status":                   domain.SubscriptionActive,
		"provider_subscription_id": event.SubscriptionID,
		"provider_customer_id":     event.ProviderCustomerID,
		"billing_cycle":            event.BillingCycle,
		"current_period_end":       periodEnd,
		"cancel_at_period_end":     false,
		"cancelled_at":             nil,
		"renewal_reminder_sent_at": nil,
		"card_last4":               event.CardLast4,
		"card_brand":               event.CardBrand,
		"card_expiry":              event.CardExpiry,
		"updated_at":               now,
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.subsCol.UpdateOne(ctx, bson.M{"host_id": event.HostID}, bson.M{
		"$set":         subUpdate,
		"$setOnInsert": bson.M{"created_at": now},
	}, opts)
	if err != nil {
		return fmt.Errorf("failed to upsert subscription: %w", err)
	}

	// 5. Upgrade host tier to match plan
	tier := "free"
	if plan != nil {
		tier = plan.Tier
	}
	if _, err := s.hostsCol.UpdateOne(ctx, bson.M{"_id": event.HostID}, bson.M{
		"$set": bson.M{"tier": tier},
	}); err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Str("tier", tier).Msg("Failed to update host tier")
	}

	return nil
}

// HandleSubscriptionStateUpdate handles subscription.active and subscription.renewed events.
// These only update the subscription status and period — no financial records are created.
func (s *BillingService) HandleSubscriptionStateUpdate(ctx context.Context, event *provider.BillingEvent, hostEmail, planName string) error {
	now := time.Now()

	update := bson.M{
		"status":                   event.Status,
		"provider_subscription_id": event.ProviderID,
		"provider_customer_id":     event.ProviderCustomerID,
		"cancelled_at":             nil, // Reactivate if renewed
		"renewal_reminder_sent_at": nil,
		"updated_at":               now,
	}

	if event.Status == domain.SubscriptionCanceled {
		update["cancelled_at"] = now
	}

	if event.CardLast4 != "" {
		update["card_last4"] = event.CardLast4
		update["card_brand"] = event.CardBrand
		update["card_expiry"] = event.CardExpiry
	}

	if event.CurrentPeriodEnd > 0 {
		update["current_period_end"] = time.Unix(event.CurrentPeriodEnd, 0)
	}
	if event.BillingCycle != "" {
		update["billing_cycle"] = event.BillingCycle
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.subsCol.UpdateOne(ctx, bson.M{"host_id": event.HostID}, bson.M{
		"$set":         update,
		"$setOnInsert": bson.M{"created_at": now, "host_id": event.HostID, "plan_id": event.PlanID},
	}, opts)

	if err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to update subscription state")
		return err
	}

	if event.Type == domain.EventSubscriptionActive || event.Type == domain.EventSubscriptionRenewed {
		_ = s.ResetMonthlyLimits(ctx, event.HostID)

		if event.Type == domain.EventSubscriptionRenewed && hostEmail != "" && s.emailSvc != nil {
			_ = s.emailSvc.SendSubscriptionRenewed(hostEmail, planName, event.BillingCycle, event.TotalAmount, event.Currency)
		}
	}

	if event.Status == "pending" && hostEmail != "" && s.emailSvc != nil {
		_ = s.emailSvc.SendSubscriptionPending(hostEmail, planName)
	}

	return nil
}

// HandleSubscriptionUpdated syncs metadata and plan changes from subscription.updated events.
func (s *BillingService) HandleSubscriptionUpdated(ctx context.Context, event *provider.BillingEvent) error {
	now := time.Now()

	update := bson.M{
		"cancel_at_period_end":  event.CancelAtPeriodEnd,
		"cancellation_comment":  event.CancellationComment,
		"cancellation_feedback": event.CancellationFeedback,
		"updated_at":            now,
	}

	if event.PlanID != "" {
		update["plan_id"] = event.PlanID
	}
	if event.BillingCycle != "" {
		update["billing_cycle"] = event.BillingCycle
	}

	if event.CardLast4 != "" {
		update["card_last4"] = event.CardLast4
		update["card_brand"] = event.CardBrand
		update["card_expiry"] = event.CardExpiry
	}

	if event.CancelledAt > 0 {
		update["cancelled_at"] = time.Unix(event.CancelledAt, 0)
	}
	if event.CurrentPeriodEnd > 0 {
		update["current_period_end"] = time.Unix(event.CurrentPeriodEnd, 0)
	}

	_, err := s.subsCol.UpdateOne(ctx, bson.M{"host_id": event.HostID}, bson.M{"$set": update})
	if err != nil {
		log.Error().Err(err).Str("host_id", event.HostID).Msg("Failed to update subscription metadata")
		return err
	}

	if event.PlanID != "" {
		plan, _ := s.GetPlanByID(ctx, event.PlanID)
		if plan != nil {
			_, _ = s.hostsCol.UpdateOne(ctx, bson.M{"_id": event.HostID}, bson.M{
				"$set": bson.M{"tier": plan.Tier},
			})
		}
	}

	s.invalidateHostPlanCache(ctx, event.HostID)

	return nil
}

// GetHostPlanName returns the plan name for a given plan ID (used in emails).
func (s *BillingService) GetHostPlanName(ctx context.Context, planID string) string {
	plan, err := s.GetPlanByID(ctx, planID)
	if err != nil || plan == nil {
		return planID
	}
	return plan.Name
}

func (s *BillingService) GetHostEmail(ctx context.Context, hostID string) (string, error) {
	var host hostdomain.Host
	if err := s.hostsCol.FindOne(ctx, bson.M{"_id": hostID}).Decode(&host); err != nil {
		return "", err
	}
	if host.Email != nil {
		return *host.Email, nil
	}
	return "", nil
}

// GetSubscriptionsExpiringIn returns subscriptions whose current period ends
// within the given duration from now. Used by the renewal reminder cron job.
func (s *BillingService) GetSubscriptionsExpiringIn(ctx context.Context, d time.Duration) ([]domain.Subscription, error) {
	now := time.Now()
	deadline := now.Add(d)

	cursor, err := s.subsCol.Find(ctx, bson.M{
		"status":                   domain.SubscriptionActive,
		"cancelled_at":             nil,
		"renewal_reminder_sent_at": nil,
		"current_period_end": bson.M{
			"$gte": now,
			"$lte": deadline,
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var subs []domain.Subscription
	if err := cursor.All(ctx, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}

// RunRenewalReminders orchestrates finding expiring subscriptions and sending emails.
func (s *BillingService) RunRenewalReminders(ctx context.Context) error {
	// Look for subscriptions expiring in 3 days
	subs, err := s.GetSubscriptionsExpiringIn(ctx, 3*24*time.Hour)
	if err != nil {
		return err
	}

	for _, sub := range subs {
		hostEmail, err := s.GetHostEmail(ctx, sub.HostID)
		if err != nil || hostEmail == "" {
			continue
		}

		plan, err := s.GetPlanByID(ctx, sub.PlanID)
		if err != nil || plan == nil {
			continue
		}

		// Calculate amount based on billing cycle
		amount := plan.MonthlyPrice
		if sub.BillingCycle == "yearly" {
			amount = plan.YearlyPrice
		}

		if s.emailSvc != nil {
			_ = s.emailSvc.SendRenewalReminder(hostEmail, plan.Name, sub.CurrentPeriodEnd, amount, plan.Currency)
		}

		// Mark as sent to prevent multiple emails
		now := time.Now()
		_, _ = s.subsCol.UpdateOne(ctx, bson.M{"host_id": sub.HostID, "cancelled_at": nil}, bson.M{
			"$set": bson.M{"renewal_reminder_sent_at": &now},
		})
	}

	return nil
}

func (s *BillingService) GetSubscription(ctx context.Context, hostID string) (*domain.Subscription, error) {
	var sub domain.Subscription
	err := s.subsCol.FindOne(ctx, bson.M{"host_id": hostID, "cancelled_at": nil}).Decode(&sub)
	if err != nil {
		return nil, err
	}

	// Handle expired grace period
	if sub.Status == domain.SubscriptionInGrace && sub.GracePeriodUntil != nil && time.Now().After(*sub.GracePeriodUntil) {
		log.Info().Str("host_id", hostID).Msg("Subscription grace period expired, treating as canceled")

		// Update DB to reflect expiration
		_, _ = s.subsCol.UpdateOne(ctx, bson.M{"host_id": hostID}, bson.M{
			"$set": bson.M{
				"status":       domain.SubscriptionCanceled,
				"cancelled_at": time.Now(),
				"updated_at":   time.Now(),
			},
		})

		_ = s.downgradeHostTier(ctx, hostID)

		return nil, mongodriver.ErrNoDocuments
	}

	return &sub, nil
}

func (s *BillingService) CancelSubscriptionAtPeriodEnd(ctx context.Context, hostID string, comment *string, feedback *string) error {
	sub, err := s.GetSubscription(ctx, hostID)
	if err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return fmt.Errorf("no active subscription found")
		}
		return fmt.Errorf("failed to check subscription: %w", err)
	}

	if sub.ProviderSubscriptionID == "" {
		return fmt.Errorf("subscription has no provider ID")
	}

	if sub.CancelAtPeriodEnd {
		return fmt.Errorf("subscription is already scheduled for cancellation")
	}

	if err := s.payment.CancelSubscription(sub.ProviderSubscriptionID, comment, feedback); err != nil {
		return fmt.Errorf("provider cancellation failed: %w", err)
	}
	now := time.Now()
	_, updateErr := s.subsCol.UpdateOne(ctx, bson.M{"host_id": hostID}, bson.M{
		"$set": bson.M{
			"cancel_at_period_end":  true,
			"cancellation_comment":  comment,
			"cancellation_feedback": feedback,
			"updated_at":            now,
		},
	})

	// Invalidate cached plan so the UI immediately reflects the cancelling state
	s.invalidateHostPlanCache(ctx, hostID)

	return updateErr
}

func (s *BillingService) UpdatePaymentMethod(ctx context.Context, hostID, returnURL string) (string, error) {
	sub, err := s.GetSubscription(ctx, hostID)
	if err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return "", fmt.Errorf("no active subscription found")
		}
		return "", fmt.Errorf("failed to check subscription: %w", err)
	}

	if sub.ProviderSubscriptionID == "" {
		return "", fmt.Errorf("subscription has no provider ID")
	}

	resp, err := s.payment.UpdatePaymentMethod(sub.ProviderSubscriptionID, returnURL)
	if err != nil {
		return "", err
	}
	return resp.PaymentLink, nil
}

// SyncSubscriptionDetails fetches the latest metadata from the provider and updates the DB.
// Useful when webhooks miss card details or for manual refreshes.
func (s *BillingService) SyncSubscriptionDetails(ctx context.Context, sub *domain.Subscription) error {
	if sub.ProviderSubscriptionID == "" {
		return nil
	}

	details, err := s.payment.GetSubscription(sub.ProviderSubscriptionID)
	if err != nil {
		log.Error().Err(err).Str("sub_id", sub.ProviderSubscriptionID).Msg("Failed to sync from provider")
		return err
	}

	now := time.Now()
	if details.CardLast4 != "" {
		sub.CardLast4 = details.CardLast4
		sub.CardBrand = details.CardBrand
		sub.CardExpiry = details.CardExpiry
	}
	if details.Status != "" {
		sub.Status = domain.SubscriptionStatus(details.Status)
	}
	if details.NextBillingDate > 0 {
		sub.CurrentPeriodEnd = time.Unix(details.NextBillingDate, 0)
	}
	sub.UpdatedAt = now

	update := bson.M{
		"card_last4":         sub.CardLast4,
		"card_brand":         sub.CardBrand,
		"card_expiry":        sub.CardExpiry,
		"status":             sub.Status,
		"current_period_end": sub.CurrentPeriodEnd,
		"updated_at":         sub.UpdatedAt,
	}
	if sub.Status == domain.SubscriptionCanceled {
		update["cancelled_at"] = now
	} else {
		update["cancelled_at"] = nil
	}

	_, err = s.subsCol.UpdateOne(ctx, bson.M{"host_id": sub.HostID}, bson.M{"$set": update})
	return err
}

func generateInvoiceNumber() string {
	now := time.Now()
	return fmt.Sprintf("QB-%s-%s", now.Format("20060102"), uuid.New().String()[:6])
}

func (s *BillingService) getStandardFreePlan(ctx context.Context) (*domain.Plan, error) {
	var plan domain.Plan
	opts := options.FindOne().SetSort(bson.M{"created_at": -1})
	err := s.plansCol.FindOne(ctx, bson.M{"tier": "free", "is_free": true}, opts).Decode(&plan)
	if err != nil {
		return nil, fmt.Errorf("no free plan found in database: %w", err)
	}
	return &plan, nil
}
