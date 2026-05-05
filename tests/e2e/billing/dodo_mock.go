// Package billing provides Dodo Payments webhook payload factories for E2E tests.
//
// These factories produce JSON payloads that match the real Dodo Payments webhook
// schema (https://docs.dodopayments.com/developer-resources/webhooks/intents/).
// Payloads are signed using the standard-webhooks library so the production
// DodoProvider.VerifyWebhook accepts them without modification.
package billing

import (
	"encoding/json"
	"time"
)

// PaymentPayload mirrors a Dodo payment webhook event.
// Ref: https://docs.dodopayments.com/developer-resources/webhooks/intents/payment
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
// Ref: https://docs.dodopayments.com/developer-resources/webhooks/intents/subscription
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
			SubscriptionID:   "sub_" + hostID[:8],
			TotalAmount:      99900,
			SettlementAmount: 97000,
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
			SubscriptionID:   "sub_" + hostID[:8],
			TotalAmount:      99900,
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
			RecurringPreTaxAmount:    99900,
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
			RecurringPreTaxAmount:    99900,
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
