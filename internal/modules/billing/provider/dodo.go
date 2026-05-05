package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"queuebuzz/internal/log"
	billingdomain "queuebuzz/internal/modules/billing/domain"

	"github.com/dodopayments/dodopayments-go"
	"github.com/dodopayments/dodopayments-go/option"
	stdwebhook "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

// DodoProvider implements PaymentProvider using the official Dodo Payments Go SDK.
type DodoProvider struct {
	client     *dodopayments.Client
	webhookKey string
	isTestMode bool
}

// NewDodoProvider creates a provider using the official SDK.
// Set isTestMode=true for development, false for production.
func NewDodoProvider(apiKey, webhookKey string, isTestMode bool) *DodoProvider {
	opts := []option.RequestOption{
		option.WithBearerToken(apiKey),
	}
	if isTestMode {
		opts = append(opts, option.WithEnvironmentTestMode())
	} else {
		opts = append(opts, option.WithEnvironmentLiveMode())
	}

	client := dodopayments.NewClient(opts...)

	return &DodoProvider{
		client:     client,
		webhookKey: webhookKey,
		isTestMode: isTestMode,
	}
}

// CreateCheckoutSession creates a hosted checkout session via the Dodo SDK.
// The user is redirected to the returned URL to complete payment.
func (p *DodoProvider) CreateCheckoutSession(req CheckoutRequest) (*CheckoutResponse, error) {
	// Resolve the correct product ID based on billing cycle
	productID := req.Plan.ProviderMonthlyProductID
	if req.BillingCycle == "yearly" {
		productID = req.Plan.ProviderYearlyProductID
	}
	if productID == "" {
		return nil, fmt.Errorf("no payment provider product ID configured for plan %s (%s cycle)",
			req.Plan.Slug, req.BillingCycle)
	}

	// Extract host email for checkout prefill
	var email string
	if req.Host.Email != nil {
		email = *req.Host.Email
	}

	// Build metadata for webhook correlation
	metadata := map[string]string{
		"host_id":       req.Host.ID,
		"plan_id":       req.Plan.ID,
		"billing_cycle": req.BillingCycle,
		"plan_tier":     req.Plan.Tier,
	}

	// Create checkout session via SDK
	params := dodopayments.CheckoutSessionNewParams{
		CheckoutSessionRequest: dodopayments.CheckoutSessionRequestParam{
			ProductCart: dodopayments.F([]dodopayments.ProductItemReqParam{
				{
					ProductID: dodopayments.F(productID),
					Quantity:  dodopayments.F(int64(1)),
				},
			}),
			ReturnURL: dodopayments.F(req.SuccessURL),
			CancelURL: dodopayments.F(req.CancelURL),
			Metadata:  dodopayments.F(metadata),
		},
	}

	// Prefill customer email if available
	if email != "" {
		params.CheckoutSessionRequest.Customer = dodopayments.F[dodopayments.CustomerRequestUnionParam](
			dodopayments.CustomerRequestParam{
				Email: dodopayments.F(email),
			},
		)
	}

	session, err := p.client.CheckoutSessions.New(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Str("plan", req.Plan.Slug).Str("host", req.Host.ID).Msg("Failed to create Dodo checkout session")
		return nil, fmt.Errorf("failed to create checkout session: %w", err)
	}

	return &CheckoutResponse{
		URL:       session.CheckoutURL,
		SessionID: session.SessionID,
	}, nil
}

func (p *DodoProvider) VerifyWebhook(payload []byte, headers map[string]string) (*BillingEvent, error) {
	if p.webhookKey == "" {
		return nil, fmt.Errorf("webhook key not configured")
	}

	// Verify signature using Standard Webhooks library
	wh, err := stdwebhook.NewWebhook(p.webhookKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize webhook verifier: %w", err)
	}

	httpHeaders := http.Header{}
	httpHeaders.Set("webhook-id", headers["webhook-id"])
	httpHeaders.Set("webhook-signature", headers["webhook-signature"])
	httpHeaders.Set("webhook-timestamp", headers["webhook-timestamp"])

	if err := wh.Verify(payload, httpHeaders); err != nil {
		return nil, fmt.Errorf("webhook signature verification failed: %w", err)
	}

	// Parse the top-level envelope
	var raw struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	event := &BillingEvent{
		Type: raw.Type,
	}

	// Route parsing based on event type prefix
	switch {
	case isPaymentEvent(raw.Type):
		if err := p.parsePaymentEvent(raw.Data, event); err != nil {
			return nil, err
		}
	case isSubscriptionEvent(raw.Type):
		if err := p.parseSubscriptionEvent(raw.Data, event); err != nil {
			return nil, err
		}
	default:
		// Unknown event type — still valid, return with raw type for logging
		log.Debug().Str("type", raw.Type).Msg("Unrecognized Dodo webhook event type")
	}

	return event, nil
}

// parsePaymentEvent extracts fields from payment.succeeded / payment.failed payloads.
// Per Dodo docs, payment webhooks include: payment_id, total_amount, customer,
// subscription_id, invoice_url, settlement_amount, currency, metadata, etc.
func (p *DodoProvider) parsePaymentEvent(data json.RawMessage, event *BillingEvent) error {
	var pd struct {
		PaymentID        string `json:"payment_id"`
		SubscriptionID   string `json:"subscription_id"`
		TotalAmount      int    `json:"total_amount"`
		SettlementAmount int    `json:"settlement_amount"`
		Currency         string `json:"currency"`
		InvoiceURL       string `json:"invoice_url"`
		Customer         struct {
			CustomerID string `json:"customer_id"`
			Email      string `json:"email"`
		} `json:"customer"`
		Metadata     map[string]string `json:"metadata"`
		CardLastFour string            `json:"card_last_four"`
		CardNetwork  string            `json:"card_network"`
		ExpiryMonth  string            `json:"expiry_month"`
		ExpiryYear   string            `json:"expiry_year"`
	}
	if err := json.Unmarshal(data, &pd); err != nil {
		return fmt.Errorf("failed to parse payment event data: %w", err)
	}

	event.ProviderPaymentID = pd.PaymentID
	event.SubscriptionID = pd.SubscriptionID
	event.ProviderID = pd.SubscriptionID // for correlation
	event.TotalAmount = pd.TotalAmount
	event.Amount = pd.SettlementAmount
	event.Currency = pd.Currency
	event.InvoiceURL = pd.InvoiceURL
	event.ProviderCustomerID = pd.Customer.CustomerID
	event.CardLast4 = pd.CardLastFour
	event.CardBrand = pd.CardNetwork
	if pd.ExpiryMonth != "" && pd.ExpiryYear != "" {
		event.CardExpiry = fmt.Sprintf("%s/%s", pd.ExpiryMonth, pd.ExpiryYear)
	}

	// Map internal event type
	switch event.Type {
	case "payment.succeeded":
		event.Type = billingdomain.EventPaymentSucceeded
		event.Status = billingdomain.SubscriptionActive
	case "payment.failed":
		event.Type = billingdomain.EventPaymentFailed
		event.Status = billingdomain.SubscriptionPastDue
	}

	// Extract host/plan info from metadata (set at checkout time)
	if pd.Metadata != nil {
		event.HostID = pd.Metadata["host_id"]
		event.PlanID = pd.Metadata["plan_id"]
		event.BillingCycle = pd.Metadata["billing_cycle"]
	}

	return nil
}

// parseSubscriptionEvent extracts fields from subscription.* payloads.
// Per Dodo docs, subscription webhooks include: subscription_id, status,
// cancel_at_next_billing_date, cancellation_comment, cancellation_feedback,
// cancelled_at, next_billing_date, recurring_pre_tax_amount, customer, metadata, etc.
func (p *DodoProvider) parseSubscriptionEvent(data json.RawMessage, event *BillingEvent) error {
	var sd struct {
		SubscriptionID           string `json:"subscription_id"`
		Status                   string `json:"status"`
		Currency                 string `json:"currency"`
		RecurringPreTaxAmount    int    `json:"recurring_pre_tax_amount"`
		NextBillingDate          string `json:"next_billing_date"`
		PreviousBillingDate      string `json:"previous_billing_date"`
		CancelAtNextBillingDate  bool   `json:"cancel_at_next_billing_date"`
		CancellationComment      string `json:"cancellation_comment"`
		CancellationFeedback     string `json:"cancellation_feedback"`
		CancelledAt              string `json:"cancelled_at"`
		PaymentFrequencyInterval string `json:"payment_frequency_interval"`
		Customer                 struct {
			CustomerID string `json:"customer_id"`
		} `json:"customer"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(data, &sd); err != nil {
		return fmt.Errorf("failed to parse subscription event data: %w", err)
	}

	event.ProviderID = sd.SubscriptionID
	event.SubscriptionID = sd.SubscriptionID
	event.Currency = sd.Currency
	event.Amount = sd.RecurringPreTaxAmount
	event.TotalAmount = sd.RecurringPreTaxAmount
	event.RecurringPreTaxAmount = sd.RecurringPreTaxAmount
	event.CancelAtPeriodEnd = sd.CancelAtNextBillingDate
	event.CancellationComment = sd.CancellationComment
	event.CancellationFeedback = sd.CancellationFeedback
	event.ProviderCustomerID = sd.Customer.CustomerID

	// Parse timestamps (Dodo sends ISO 8601)
	if t, err := parseISO8601(sd.NextBillingDate); err == nil {
		event.NextBillingDate = t.Unix()
		event.CurrentPeriodEnd = t.Unix()
	}
	if t, err := parseISO8601(sd.CancelledAt); err == nil {
		event.CancelledAt = t.Unix()
	}
	if t, err := parseISO8601(sd.PreviousBillingDate); err == nil {
		event.PreviousBillingDate = t.Unix()
	}

	// Map internal event type and status
	switch event.Type {
	case "subscription.active":
		event.Type = billingdomain.EventSubscriptionActive
		event.Status = billingdomain.SubscriptionActive
	case "subscription.renewed":
		event.Type = billingdomain.EventSubscriptionRenewed
		event.Status = billingdomain.SubscriptionActive
	case "subscription.failed", "subscription.on_hold":
		event.Type = billingdomain.EventSubscriptionFailed
		event.Status = billingdomain.SubscriptionPastDue
	case "subscription.cancelled":
		event.Type = billingdomain.EventSubscriptionCancelled
		event.Status = billingdomain.SubscriptionCanceled
	case "subscription.updated":
		event.Type = billingdomain.EventSubscriptionUpdated
		event.Status = billingdomain.SubscriptionActive
	case "subscription.expired":
		event.Type = billingdomain.EventSubscriptionCancelled
		event.Status = billingdomain.SubscriptionCanceled
	default:
		// Keep raw type for unhandled sub-events
	}

	// Extract host/plan info from metadata (set at checkout time)
	if sd.Metadata != nil {
		event.HostID = sd.Metadata["host_id"]
		event.PlanID = sd.Metadata["plan_id"]
		event.BillingCycle = sd.Metadata["billing_cycle"]
	}

	// Infer billing cycle from payment_frequency_interval if metadata is missing
	if event.BillingCycle == "" {
		switch sd.PaymentFrequencyInterval {
		case "Year":
			event.BillingCycle = "yearly"
		default:
			event.BillingCycle = "monthly"
		}
	}

	// Fetch latest details to get card info (Dodo webhooks for subscriptions don't include it)
	if details, err := p.GetSubscription(sd.SubscriptionID); err == nil {
		event.CardLast4 = details.CardLast4
		event.CardBrand = details.CardBrand
		event.CardExpiry = details.CardExpiry
	} else {
		log.Warn().Err(err).Str("subscription_id", sd.SubscriptionID).Msg("Failed to fetch subscription details during webhook parsing")
	}

	return nil
}

func (p *DodoProvider) GetSubscription(subscriptionID string) (*SubscriptionDetails, error) {
	sub, err := p.client.Subscriptions.Get(context.Background(), subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	details := &SubscriptionDetails{
		SubscriptionID:           sub.SubscriptionID,
		Status:                   string(sub.Status),
		CustomerID:               sub.Customer.CustomerID,
		ProductID:                sub.ProductID,
		Currency:                 string(sub.Currency),
		NextBillingDate:          sub.NextBillingDate.Unix(),
		PreviousBillingDate:      sub.PreviousBillingDate.Unix(),
		RecurringPreTaxAmount:    int(sub.RecurringPreTaxAmount),
		CancelAtPeriodEnd:        sub.CancelAtNextBillingDate,
		CancellationComment:      sub.CancellationComment,
		CancellationFeedback:     string(sub.CancellationFeedback),
		PaymentFrequencyInterval: string(sub.PaymentFrequencyInterval),
		Metadata:                 sub.Metadata,
	}

	// Fetch payment method details if ID is present
	if sub.PaymentMethodID != "" {
		pms, err := p.client.Customers.GetPaymentMethods(context.Background(), sub.Customer.CustomerID)
		if err == nil {
			for _, pm := range pms.Items {
				if pm.PaymentMethodID == sub.PaymentMethodID {
					details.CardLast4 = pm.Card.Last4Digits
					details.CardBrand = pm.Card.CardNetwork
					details.CardExpiry = fmt.Sprintf("%v/%v", pm.Card.ExpiryMonth, pm.Card.ExpiryYear)
					break
				}
			}
		}
	}

	if !sub.CancelledAt.IsZero() {
		details.CancelledAt = sub.CancelledAt.Unix()
	}

	return details, nil
}

// CancelSubscription cancels a subscription at the end of the current billing period.
func (p *DodoProvider) CancelSubscription(subscriptionID string, comment *string, feedback *string) error {
	params := dodopayments.SubscriptionUpdateParams{
		CancelAtNextBillingDate: dodopayments.F(true),
	}

	if comment != nil {
		params.CancellationComment = dodopayments.F(*comment)
	}
	if feedback != nil && *feedback != "" {
		var dodoFeedback dodopayments.CancellationFeedback
		switch *feedback {
		case "too_expensive":
			dodoFeedback = dodopayments.CancellationFeedbackTooExpensive
		case "missing_features":
			dodoFeedback = dodopayments.CancellationFeedbackMissingFeatures
		case "switching", "switching_to_competitor", "switched_service":
			dodoFeedback = dodopayments.CancellationFeedbackSwitchedService
		case "not_using", "unused":
			dodoFeedback = dodopayments.CancellationFeedbackUnused
		default:
			// If it doesn't match a known enum, we don't send the feedback field
			// to avoid 422, but the reason will still be in the comment.
		}

		if dodoFeedback != "" {
			params.CancellationFeedback = dodopayments.F(dodoFeedback)
		}
	}

	_, err := p.client.Subscriptions.Update(context.Background(), subscriptionID, params)
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}
	return nil
}

// UpdatePaymentMethod returns a Dodo-hosted payment link for updating the payment method.
func (p *DodoProvider) UpdatePaymentMethod(subscriptionID, returnURL string) (*PaymentMethodUpdateResponse, error) {
	params := dodopayments.SubscriptionUpdatePaymentMethodParams{
		Body: dodopayments.SubscriptionUpdatePaymentMethodParamsBodyNew{
			Type:      dodopayments.F(dodopayments.SubscriptionUpdatePaymentMethodParamsBodyNewTypeNew),
			ReturnURL: dodopayments.F(returnURL),
		},
	}

	resp, err := p.client.Subscriptions.UpdatePaymentMethod(context.Background(), subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("failed to update payment method: %w", err)
	}

	return &PaymentMethodUpdateResponse{
		PaymentLink: resp.PaymentLink,
	}, nil
}

func isPaymentEvent(eventType string) bool {
	return len(eventType) > 8 && eventType[:8] == "payment."
}

func isSubscriptionEvent(eventType string) bool {
	return len(eventType) > 13 && eventType[:13] == "subscription."
}

func parseISO8601(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	return time.Parse(time.RFC3339, s)
}
