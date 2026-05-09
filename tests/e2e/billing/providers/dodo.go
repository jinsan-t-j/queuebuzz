package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	billingprovider "queuebuzz/internal/modules/billing/provider"
	"sync"
	"time"
)

// DodoMockServer provides an in-process HTTP server to mock Dodo Payments API responses.
type DodoMockServer struct {
	Server        *httptest.Server
	mu            sync.RWMutex
	Subscriptions map[string]*billingprovider.SubscriptionDetails
	CheckoutURL   string
	PaymentLink   string
	Err           error
}

func NewDodoMockServer() *DodoMockServer {
	m := &DodoMockServer{
		Subscriptions: make(map[string]*billingprovider.SubscriptionDetails),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handleRequest))
	m.CheckoutURL = "https://dodopayments.com/checkout/mock"
	m.PaymentLink = "https://dodopayments.com/update/mock"
	return m
}

func (m *DodoMockServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Err = nil
	m.Subscriptions = make(map[string]*billingprovider.SubscriptionDetails)
	m.CheckoutURL = "https://dodopayments.com/checkout/mock"
	m.PaymentLink = "https://dodopayments.com/update/mock"
}

func (m *DodoMockServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if m.Err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": m.Err.Error()})
		return
	}

	// Route based on path
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/checkouts":
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"checkout_url":"%s","session_id":"mock_session_123"}`, m.CheckoutURL)

	case r.Method == http.MethodGet && len(r.URL.Path) > 15 && r.URL.Path[:15] == "/subscriptions/":
		subID := r.URL.Path[15:]
		w.WriteHeader(http.StatusOK)
		if sub, ok := m.Subscriptions[subID]; ok {
			_ = json.NewEncoder(w).Encode(sub)
			return
		}
		_, _ = fmt.Fprintf(w, `{
			"subscription_id": "%s",
			"status": "active",
			"customer": {"customer_id": "cust_123"},
			"product_id": "prod_123",
			"currency": "INR",
			"next_billing_date": "2026-06-01T00:00:00Z",
			"recurring_pre_tax_amount": 49900
		}`, subID)

	case r.Method == http.MethodPatch && len(r.URL.Path) > 15 && r.URL.Path[:15] == "/subscriptions/":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "updated"}`))

	case r.Method == http.MethodPost && len(r.URL.Path) > 15 && r.URL.Path[len(r.URL.Path)-22:] == "/update-payment-method":
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"payment_link": "%s"}`, m.PaymentLink)

	case r.Method == http.MethodGet && len(r.URL.Path) > 11 && r.URL.Path[:11] == "/customers/":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"payment_method_id": "pm_123",
					"card": {
						"last4_digits": "4242",
						"card_network": "Visa",
						"expiry_month": 12,
						"expiry_year": 2028
					}
				}
			]
		}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// AutoSeed peeks into a Dodo webhook payload and registers the subscription state.
func (m *DodoMockServer) AutoSeed(payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var envelope struct {
		Type string `json:"type"`
		Data struct {
			SubscriptionID        string `json:"subscription_id"`
			Status                string `json:"status"`
			Currency              string `json:"currency"`
			RecurringPreTaxAmount int    `json:"recurring_pre_tax_amount"`
			NextBillingDate       string `json:"next_billing_date"`
			Customer              struct {
				CustomerID string `json:"customer_id"`
			} `json:"customer"`
			Metadata map[string]string `json:"metadata"`
		} `json:"data"`
	}

	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}

	if envelope.Data.SubscriptionID == "" {
		return
	}

	status := envelope.Data.Status
	if envelope.Type == "subscription.failed" {
		status = "on_hold"
	}

	sub := &billingprovider.SubscriptionDetails{
		SubscriptionID:        envelope.Data.SubscriptionID,
		Status:                status,
		CustomerID:            envelope.Data.Customer.CustomerID,
		Currency:              envelope.Data.Currency,
		RecurringPreTaxAmount: envelope.Data.RecurringPreTaxAmount,
		Metadata:              envelope.Data.Metadata,
		CardLast4:             "4242",
		CardBrand:             "Visa",
		CardExpiry:            "12/2028",
	}

	if envelope.Data.NextBillingDate != "" {
		if t, err := time.Parse(time.RFC3339, envelope.Data.NextBillingDate); err == nil {
			sub.NextBillingDate = t.Unix()
		}
	}

	m.Subscriptions[envelope.Data.SubscriptionID] = sub
}
