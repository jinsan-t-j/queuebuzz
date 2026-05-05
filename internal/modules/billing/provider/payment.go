package provider

import (
	billingdomain "queuebuzz/internal/modules/billing/domain"
	hostdomain "queuebuzz/internal/modules/host/domain"
)

type CheckoutRequest struct {
	Host         hostdomain.Host
	Plan         billingdomain.Plan
	BillingCycle string
	SuccessURL   string
	CancelURL    string
}

type CheckoutResponse struct {
	URL       string `json:"url"`
	SessionID string `json:"session_id"`
}

type BillingEvent struct {
	Type              string
	ProviderID        string // Dodo subscription ID
	HostID            string // Mapped from customer metadata
	PlanID            string // Mapped from product metadata
	BillingCycle      string
	Status            billingdomain.SubscriptionStatus
	Amount            int
	Currency          string
	ProviderPaymentID string
	CurrentPeriodEnd  int64

	// Payment-specific fields (from payment.succeeded / payment.failed)
	TotalAmount        int
	ProviderCustomerID string
	InvoiceURL         string
	SubscriptionID     string

	// Subscription-specific fields (from subscription.* webhooks)
	CancelAtPeriodEnd     bool
	CancellationComment   string
	CancellationFeedback  string
	CancelledAt           int64
	NextBillingDate       int64
	PreviousBillingDate   int64
	RecurringPreTaxAmount int

	CardLast4  string
	CardBrand  string
	CardExpiry string
}

type PaymentProvider interface {
	CreateCheckoutSession(req CheckoutRequest) (*CheckoutResponse, error)

	VerifyWebhook(payload []byte, headers map[string]string) (*BillingEvent, error)

	GetSubscription(subscriptionID string) (*SubscriptionDetails, error)

	CancelSubscription(subscriptionID string, comment *string, feedback *string) error

	UpdatePaymentMethod(subscriptionID, returnURL string) (*PaymentMethodUpdateResponse, error)
}

type SubscriptionDetails struct {
	SubscriptionID           string
	Status                   string
	CustomerID               string
	ProductID                string
	Currency                 string
	NextBillingDate          int64
	PreviousBillingDate      int64
	RecurringPreTaxAmount    int
	CancelAtPeriodEnd        bool
	CancellationComment      string
	CancellationFeedback     string
	CancelledAt              int64
	PaymentFrequencyInterval string
	CardLast4                string
	CardBrand                string
	CardExpiry               string
	Metadata                 map[string]string
}

type PaymentMethodUpdateResponse struct {
	PaymentLink string
}
