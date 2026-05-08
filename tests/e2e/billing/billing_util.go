package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	stdwebhook "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// PaymentPayload mirrors a Dodo payment webhook event.
type PaymentPayload struct {
	Type string      `json:"type"`
	Data PaymentData `json:"data"`
}

type PaymentData struct {
	PaymentID        string            `json:"payment_id"`
	SubscriptionID   string            `json:"subscription_id"`
	TotalAmount      int               `json:"total_amount"`
	SettlementAmount int               `json:"settlement_amount"`
	Currency         string            `json:"currency"`
	InvoiceURL       string            `json:"invoice_url,omitempty"`
	CardLastFour     string            `json:"card_last_four,omitempty"`
	CardNetwork      string            `json:"card_network,omitempty"`
	ExpiryMonth      string            `json:"expiry_month,omitempty"`
	ExpiryYear       string            `json:"expiry_year,omitempty"`
	Customer         PaymentCustomer   `json:"customer"`
	Metadata         map[string]string `json:"metadata"`
	CreatedAt        string            `json:"created_at,omitempty"`
}

type PaymentCustomer struct {
	CustomerID string `json:"customer_id"`
	Email      string `json:"email,omitempty"`
}

// SubscriptionPayload mirrors a Dodo subscription webhook event.
type SubscriptionPayload struct {
	Type string           `json:"type"`
	Data SubscriptionData `json:"data"`
}

type SubscriptionData struct {
	SubscriptionID           string            `json:"subscription_id"`
	Status                   string            `json:"status"`
	Currency                 string            `json:"currency,omitempty"`
	RecurringPreTaxAmount    int               `json:"recurring_pre_tax_amount,omitempty"`
	NextBillingDate          string            `json:"next_billing_date,omitempty"`
	PreviousBillingDate      string            `json:"previous_billing_date,omitempty"`
	CancelAtNextBillingDate  bool              `json:"cancel_at_next_billing_date"`
	CancellationComment      string            `json:"cancellation_comment,omitempty"`
	CancellationFeedback     string            `json:"cancellation_feedback,omitempty"`
	CancelledAt              string            `json:"cancelled_at,omitempty"`
	PaymentFrequencyInterval string            `json:"payment_frequency_interval,omitempty"`
	Customer                 SubscriptionCust  `json:"customer"`
	Metadata                 map[string]string `json:"metadata"`
}

type SubscriptionCust struct {
	CustomerID string `json:"customer_id"`
}

// PaymentSucceeded builds a payment.succeeded webhook payload.
func PaymentSucceeded(hostID, planID, billingCycle string) []byte {
	p := PaymentPayload{
		Type: "payment.succeeded",
		Data: PaymentData{
			PaymentID:        "pay_" + hostID[:8],
			SubscriptionID:   "sub-" + hostID,
			TotalAmount:      49900,
			SettlementAmount: 48000,
			Currency:         "INR",
			InvoiceURL:       "https://dodo.test/invoice/inv_test_123",
			CardLastFour:     "4242",
			CardNetwork:      "Visa",
			ExpiryMonth:      "12",
			ExpiryYear:       "2028",
			Customer:         PaymentCustomer{CustomerID: "cust_" + hostID[:8], Email: hostID},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
				"plan_tier":     "pro",
			},
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// PaymentFailed builds a payment.failed webhook payload.
func PaymentFailed(hostID, planID, billingCycle string) []byte {
	p := PaymentPayload{
		Type: "payment.failed",
		Data: PaymentData{
			PaymentID:        "pay_fail_" + hostID[:8],
			SubscriptionID:   "sub-" + hostID,
			TotalAmount:      49900,
			SettlementAmount: 0,
			Currency:         "INR",
			CardLastFour:     "1234",
			CardNetwork:      "Mastercard",
			Customer:         PaymentCustomer{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// SubscriptionActive builds a subscription.active webhook payload.
func SubscriptionActive(hostID, planID, billingCycle string) []byte {
	nextBilling := time.Now().AddDate(0, 1, 0).UTC().Format(time.RFC3339)
	p := SubscriptionPayload{
		Type: "subscription.active",
		Data: SubscriptionData{
			SubscriptionID:           "sub_" + hostID[:8],
			Status:                   "active",
			Currency:                 "INR",
			RecurringPreTaxAmount:    49900,
			NextBillingDate:          nextBilling,
			PaymentFrequencyInterval: "Month",
			Customer:                 SubscriptionCust{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// SubscriptionRenewed builds a subscription.renewed webhook payload.
func SubscriptionRenewed(hostID, planID, billingCycle string) []byte {
	now := time.Now().UTC()
	p := SubscriptionPayload{
		Type: "subscription.renewed",
		Data: SubscriptionData{
			SubscriptionID:           "sub_" + hostID[:8],
			Status:                   "active",
			Currency:                 "INR",
			RecurringPreTaxAmount:    49900,
			NextBillingDate:          now.AddDate(0, 1, 0).Format(time.RFC3339),
			PreviousBillingDate:      now.AddDate(0, -1, 0).Format(time.RFC3339),
			PaymentFrequencyInterval: "Month",
			Customer:                 SubscriptionCust{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// SubscriptionUpdated builds a subscription.updated webhook with cancel metadata.
func SubscriptionUpdated(hostID, planID, billingCycle, comment, feedback string) []byte {
	p := SubscriptionPayload{
		Type: "subscription.updated",
		Data: SubscriptionData{
			SubscriptionID:          "sub_" + hostID[:8],
			Status:                  "active",
			CancelAtNextBillingDate: true,
			CancellationComment:     comment,
			CancellationFeedback:    feedback,
			NextBillingDate:         time.Now().AddDate(0, 1, 0).UTC().Format(time.RFC3339),
			Customer:                SubscriptionCust{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// SubscriptionCancelled builds a subscription.cancelled webhook payload.
func SubscriptionCancelled(hostID, planID, billingCycle string) []byte {
	p := SubscriptionPayload{
		Type: "subscription.cancelled",
		Data: SubscriptionData{
			SubscriptionID:          "sub_" + hostID[:8],
			Status:                  "cancelled",
			CancelAtNextBillingDate: true,
			CancelledAt:             time.Now().UTC().Format(time.RFC3339),
			Customer:                SubscriptionCust{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// SubscriptionFailed builds a subscription.failed (on_hold) webhook payload.
func SubscriptionFailed(hostID, planID, billingCycle string) []byte {
	p := SubscriptionPayload{
		Type: "subscription.failed",
		Data: SubscriptionData{
			SubscriptionID: "sub_" + hostID[:8],
			Status:         "on_hold",
			Customer:       SubscriptionCust{CustomerID: "cust_" + hostID[:8]},
			Metadata: map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// UpgradeToProPlan follows the full lifecycle: Discover -> Checkout -> Webhook
func UpgradeToProPlan(t *testing.T, s *setup.TestSuite, hostID string, token string) {
	t.Helper()

	// 1. Discover Plans (Lifecycle step 1)
	resp, err := util.GET(s, "/api/v1/billing/plans")
	require.NoError(t, err)
	resp.Body.Close()

	// 2. Create Checkout (Lifecycle step 2)
	payload := map[string]any{"plan_id": "pro-in-v1", "billing_cycle": "monthly"}
	resp, err = util.POST(s, "/api/v1/billing/checkout", payload, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Webhook Callback (Lifecycle step 3)
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")
}

// UpgradeToPremium follows the same lifecycle but for a generic premium plan
func UpgradeToPremium(t *testing.T, s *setup.TestSuite, hostID string, token string) {
	t.Helper()

	// 1. Discover Plans
	resp, err := util.GET(s, "/api/v1/billing/plans")
	require.NoError(t, err)
	resp.Body.Close()

	// 2. Create Checkout
	payload := map[string]any{"plan_id": "pro-in-v1", "billing_cycle": "monthly"}
	resp, err = util.POST(s, "/api/v1/billing/checkout", payload, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()

	// 3. Webhook Callback
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")
}

// ProvisionSubscription simulates a successful payment via webhook to create a subscription.
func ProvisionSubscription(t *testing.T, s *setup.TestSuite, hostID, planID, billingCycle string) {
	t.Helper()
	// Using explicit raw payload to test against the real webhook contract
	payload := map[string]any{
		"type": "payment.succeeded",
		"data": map[string]any{
			"payment_id":      "pay_init_" + hostID[:4],
			"subscription_id": "sub-" + hostID,
			"total_amount":    49900,
			"currency":        "INR",
			"metadata": map[string]string{
				"host_id":       hostID,
				"plan_id":       planID,
				"billing_cycle": billingCycle,
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// DowngradeToFree removes the subscription and clears billing cache.
func DowngradeToFree(t *testing.T, s *setup.TestSuite, hostID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = s.DB.Collection("billing_subscriptions").DeleteMany(ctx, bson.M{"host_id": hostID})
	s.App.Container.Redis.Del(ctx, "billing:host_plan:"+hostID)
	// Downgrade host tier
	_, _ = s.DB.Collection("hosts").UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{"$set": bson.M{"tier": "free"}})
}

// UpgradeToElite assigns the host the seeded elite-v1 plan via webhook.
func UpgradeToElite(t *testing.T, s *setup.TestSuite, hostID string) {
	t.Helper()
	ProvisionSubscription(t, s, hostID, "elite-v1", "monthly")
}

// SignWebhook signs a payload using standard-webhooks and returns headers.
func SignWebhook(t *testing.T, key string, payload []byte) map[string]string {
	t.Helper()
	wh, err := stdwebhook.NewWebhook(key)
	require.NoError(t, err)

	webhookID := "wh_" + fmt.Sprintf("%d", time.Now().UnixNano())
	timestamp := time.Now()

	signature, err := wh.Sign(webhookID, timestamp, payload)
	require.NoError(t, err)

	return map[string]string{
		"webhook-id":        webhookID,
		"webhook-signature": signature,
		"webhook-timestamp": fmt.Sprintf("%d", timestamp.Unix()),
	}
}

// SendWebhook signs a payload with the test webhook key and POSTs it to the
// billing webhook endpoint. Returns the HTTP response for assertion.
func SendWebhook(t *testing.T, s *setup.TestSuite, payload []byte) *http.Response {
	t.Helper()
	const testWebhookKey = "whsec_dGVzdF93ZWJob29rX2tleV8xMjM0NTY3ODkwMTI="
	headers := SignWebhook(t, testWebhookKey, payload)

	// Auto-seed the mock server so GetSubscription calls work without manual configuration
	s.DodoMock.AutoSeed(payload)

	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/webhook", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.Do(req)
	require.NoError(t, err)
	return resp
}
