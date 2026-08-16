package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	helpers "queuebuzz/internal/helpers"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// --- Billing test helpers (API-based) ---

// hostTier returns the host's current tier via GET /api/v1/host/me (normalized to lowercase).
func hostTier(t *testing.T, s *setup.TestSuite, token string) string {
	t.Helper()
	resp, err := util.GET(s, "/api/v1/host/me", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	data := util.DecodedBody(t, resp)
	tier, _ := data["tier"].(string)
	return strings.ToLower(tier)
}

// hostMonthlyCount returns the monthly_queue_count from host profile.
func hostMonthlyCount(t *testing.T, s *setup.TestSuite, token string) float64 {
	t.Helper()
	resp, err := util.GET(s, "/api/v1/host/me", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	data := util.DecodedBody(t, resp)
	count, _ := data["monthly_queue_count"].(float64)
	return count
}

// subStatus returns the subscription status via GET /api/v1/billing/subscription.
func subStatus(t *testing.T, s *setup.TestSuite, token string) (string, map[string]any) {
	t.Helper()
	resp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "none", nil
	}
	require.Equal(t, http.StatusOK, resp.StatusCode)
	data := util.DecodedBody(t, resp)
	status, _ := data["status"].(string)
	return status, data
}

// TEST_SETUP: countDocs for billing_transactions has no public API
func countDocs(t *testing.T, s *setup.TestSuite, collection string, filter bson.M) int64 {
	t.Helper()
	count, err := s.DB.Collection(collection).CountDocuments(context.Background(), filter)
	require.NoError(t, err)
	return count
}

func plansContainTierCurrency(plans []any, tier, currency string) bool {
	for _, p := range plans {
		pm := p.(map[string]any)
		if pm["tier"] == tier && pm["currency"] == currency {
			return true
		}
	}
	return false
}

func decodePlans(t *testing.T, resp *http.Response) []any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	data := m["data"].(map[string]any)
	return data["plans"].([]any)
}

// --- Plan Listing ---

func TestBilling_PlanListing(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	t.Run("Default country (IN)", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		plans := decodePlans(t, resp)
		assert.GreaterOrEqual(t, len(plans), 2)
		assert.True(t, plansContainTierCurrency(plans, "pro", "INR"), "Pro plan for IN not found")
	})

	t.Run("Explicit country (US)", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans?country=US")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		plans := decodePlans(t, resp)
		assert.True(t, plansContainTierCurrency(plans, "pro", "USD"), "Pro plan for US not found")
	})

	t.Run("Trial fields are never exposed in the public listing", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		for _, p := range decodePlans(t, resp) {
			pm := p.(map[string]any)
			assert.NotContains(t, pm, "trial_enabled")
			assert.NotContains(t, pm, "trial_duration_days")
			assert.NotContains(t, pm, "trial_access_token")
		}
	})
}

// --- Trial Offer Resolution ---

func TestBilling_TrialOffer(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	t.Run("Valid token resolves the plan it unlocks", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans/trial-offer?token=test-trial-token")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		data := util.DecodedBody(t, resp)
		assert.Equal(t, "elite-v1", data["plan_id"])
		assert.Equal(t, float64(3), data["trial_duration_days"])
	})

	t.Run("Invalid token falls back to 404", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans/trial-offer?token=not-a-real-token")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Missing token falls back to 404", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/plans/trial-offer")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// --- Current Plan ---

func TestBilling_CurrentPlan(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	t.Run("Unauthenticated returns 200 with default free plan", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/current-plan")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		data := util.DecodedBody(t, resp)
		assert.NotEmpty(t, data["plan"])
		planMap := data["plan"].(map[string]any)
		assert.Equal(t, "free", planMap["tier"])
	})

	t.Run("Authenticated no subscription returns free", func(t *testing.T) {
		token, _, _ := authutil.RegisterHost(t, s, "Plan Host", "plan@test.com", "password")
		resp, err := util.GET(s, "/api/v1/billing/current-plan", util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, resp.StatusCode)
	})
}

// --- Checkout Flow ---

func TestBilling_CheckoutFlow(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Setup Mock
	s.DodoMock.Reset()

	hostToken, hostID, _ := authutil.RegisterHost(t, s, "Billing Host", "billing@test.com", "password")

	t.Run("Create checkout for Pro plan", func(t *testing.T) {
		payload := map[string]any{"plan_id": "pro-in-v1", "billing_cycle": "monthly"}
		resp, err := util.POST(s, "/api/v1/billing/checkout", payload, util.AuthCookie(hostToken))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		data := util.DecodedBody(t, resp)
		assert.Contains(t, data["url"], "dodopayments.com/checkout")
	})

	t.Run("Block checkout for Free plan", func(t *testing.T) {
		payload := map[string]any{"plan_id": "free-v1"}
		resp, err := util.POST(s, "/api/v1/billing/checkout", payload, util.AuthCookie(hostToken))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Auth required", func(t *testing.T) {
		payload := map[string]any{"plan_id": "pro-in-v1"}
		resp, err := util.POST(s, "/api/v1/billing/checkout", payload)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Invalid plan", func(t *testing.T) {
		payload := map[string]any{"plan_id": "non-existent"}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Already subscribed", func(t *testing.T) {
		ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")
		payload := map[string]any{"plan_id": "pro-in-v1", "billing_cycle": "monthly"}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// --- Trial Checkout (plan.trial_enabled + trial_duration_days) ---

func TestBilling_TrialCheckout(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	s.DodoMock.Reset()

	t.Run("Trial checkout succeeds for a plan with a trial offer", func(t *testing.T) {
		hostToken, _, _ := authutil.RegisterHost(t, s, "Trial Host 1", "trial1@test.com", "password")
		payload := map[string]any{"plan_id": "elite-v1", "billing_cycle": "monthly", "is_trial": true}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		data := util.DecodedBody(t, resp)
		assert.Contains(t, data["url"], "dodopayments.com/checkout")
	})

	t.Run("Trial rejected for a plan without a trial offer", func(t *testing.T) {
		hostToken, _, _ := authutil.RegisterHost(t, s, "Trial Host 2", "trial2@test.com", "password")
		payload := map[string]any{"plan_id": "pro-in-v1", "billing_cycle": "monthly", "is_trial": true}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Trial rejected once already used for that plan", func(t *testing.T) {
		hostToken, hostID, _ := authutil.RegisterHost(t, s, "Trial Host 3", "trial3@test.com", "password")
		ProvisionSubscription(t, s, hostID, "elite-v1", "monthly")

		payload := map[string]any{"plan_id": "elite-v1", "billing_cycle": "monthly", "is_trial": true}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Non-trial checkout for the same plan is unaffected", func(t *testing.T) {
		hostToken, _, _ := authutil.RegisterHost(t, s, "Trial Host 4", "trial4@test.com", "password")
		payload := map[string]any{"plan_id": "elite-v1", "billing_cycle": "monthly"}
		resp, err := util.POSTAuth(s, "/api/v1/billing/checkout", payload, hostToken)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// --- Webhook Processing (all event types) ---

func TestBilling_WebhookPaymentSucceeded(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	token, hostID, _ := authutil.RegisterHost(t, s, "PaySuc Host", "paysuc@test.com", "password")

	// 1. Send payment.succeeded webhook
	payload := map[string]any{
		"type": "payment.succeeded",
		"data": map[string]any{
			"payment_id":      "pay_test_123",
			"subscription_id": "sub_test_123",
			"total_amount":    49900,
			"currency":        "INR",
			"metadata": map[string]string{
				"host_id":       hostID,
				"plan_id":       "pro-in-v1",
				"billing_cycle": "monthly",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify state via public API
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, "active", data["status"])
	assert.Equal(t, "pro", data["tier"])

	// 3. Verify side effects (transactions/records)
	assert.Equal(t, int64(1), countDocs(t, s, "billing_transactions", bson.M{"host_id": hostID, "status": "succeeded"}))
	assert.Equal(t, int64(1), countDocs(t, s, "billing_records", bson.M{"host_id": hostID}))
}

func TestBilling_WebhookPaymentFailed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	token, hostID, _ := authutil.RegisterHost(t, s, "PayFail Host", "payfail@test.com", "password")
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

	// 1. Send payment.failed webhook
	payload := map[string]any{
		"type": "payment.failed",
		"data": map[string]any{
			"payment_id":      "pay_fail_123",
			"subscription_id": "sub-" + hostID,
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify state via public API (should be in_grace)
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, "in_grace", data["status"])
	assert.Equal(t, "pro", data["tier"])

	util.AssertEmailReceived(t, "payfail@test.com", "failed")
}

func TestBilling_WebhookSubscriptionPending(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	token, hostID, _ := authutil.RegisterHost(t, s, "Pending Host", "pending@test.com", "password")

	// 1. Send subscription.active with status=pending (common for some providers)
	payload := map[string]any{
		"type": "subscription.active",
		"data": map[string]any{
			"subscription_id": "sub_pending_123",
			"status":          "pending",
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify state via public API
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, "pending", data["status"])

	util.AssertEmailReceived(t, "pending@test.com", "processing")
}

func TestBilling_WebhookSubscriptionFailed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "SubFail Host", "subfail@test.com", "password")
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

	// Subscription state will be updated via AutoSeed in SendWebhook below

	payload := map[string]any{
		"type": "subscription.failed",
		"data": map[string]any{
			"subscription_id": "sub-" + hostID,
			"status":          "on_hold", // Dodo status for failed payments
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify state via public API endpoint
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, "in_grace", data["status"])
}

func TestBilling_WebhookSubscriptionActive(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "SubAct Host", "subact@test.com", "password")

	payload := map[string]any{
		"type": "subscription.active",
		"data": map[string]any{
			"subscription_id": "sub-" + hostID,
			"status":          "active",
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify state via public API
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, "active", data["status"])
}

func TestBilling_WebhookSubscriptionCancelled(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	token, hostID, _ := authutil.RegisterHost(t, s, "SubCan Host", "subcan@test.com", "password")
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

	// 1. Send subscription.cancelled webhook
	payload := map[string]any{
		"type": "subscription.cancelled",
		"data": map[string]any{
			"subscription_id": "sub-" + hostID,
			"status":          "cancelled",
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify state via public API (should be 404 now)
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, apiResp.StatusCode)

	util.AssertEmailReceived(t, "subcan@test.com", "cancel")
}

func TestBilling_WebhookSubscriptionUpdated(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "SubUpd Host", "subupd@test.com", "password")
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

	// 1. Send subscription.updated webhook (e.g. user set to cancel at period end)
	payload := map[string]any{
		"type": "subscription.updated",
		"data": map[string]any{
			"subscription_id":             "sub-" + hostID,
			"status":                      "active",
			"cancel_at_next_billing_date": true,
			"cancellation_comment":        "Too expensive",
			"metadata": map[string]string{
				"host_id": hostID,
				"plan_id": "pro-in-v1",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp := SendWebhook(t, s, payloadBytes)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify state via public API
	apiResp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
	require.NoError(t, err)
	defer apiResp.Body.Close()

	var result helpers.SuccessResponse
	err = json.NewDecoder(apiResp.Body).Decode(&result)
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.Equal(t, true, data["cancel_at_period_end"])
}

// --- Webhook Security ---

func TestBilling_WebhookSecurity(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	_, hostID, _ := authutil.RegisterHost(t, s, "Sec Host", "sec@test.com", "password")
	payload := PaymentSucceeded(hostID, "pro-in-v1", "monthly")

	t.Run("Missing signature returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/webhook", nil)
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Invalid signature returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/webhook", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("webhook-id", "wh_bad")
		req.Header.Set("webhook-signature", "v1,badsignature")
		req.Header.Set("webhook-timestamp", "1234567890")
		resp, err := s.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Valid signature returns 200", func(t *testing.T) {
		resp := SendWebhook(t, s, payload)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// --- Idempotency ---

func TestBilling_WebhookIdempotency(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	_, hostID, _ := authutil.RegisterHost(t, s, "Idem Host", "idem@test.com", "password")

	payload := PaymentSucceeded(hostID, "pro-in-v1", "monthly")

	const testWebhookKey = "whsec_dGVzdF93ZWJob29rX2tleV8xMjM0NTY3ODkwMTI="
	headers := SignWebhook(t, testWebhookKey, payload)

	sendWithHeaders := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/webhook", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := s.Do(req)
		require.NoError(t, err)
		return resp
	}

	resp1 := sendWithHeaders()
	resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	resp2 := sendWithHeaders()
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	assert.Equal(t, int64(1), countDocs(t, s, "billing_transactions", bson.M{"host_id": hostID}))
}

// --- Full Lifecycle: Purchase → Renew → Cancel → Downgrade ---

func TestBilling_FullLifecycle(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)
	token, hostID, _ := authutil.RegisterHost(t, s, "Lifecycle Host", "lifecycle@test.com", "password")

	// Step 1: Initial purchase
	resp := SendWebhook(t, s, PaymentSucceeded(hostID, "pro-in-v1", "monthly"))
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, "pro", hostTier(t, s, token), "Step 1: tier should be pro")

	// Step 2: Subscription active confirmation
	resp = SendWebhook(t, s, SubscriptionActive(hostID, "pro-in-v1", "monthly"))
	resp.Body.Close()

	// Step 3: Renewal
	resp = SendWebhook(t, s, SubscriptionRenewed(hostID, "pro-in-v1", "monthly"))
	resp.Body.Close()

	assert.Equal(t, float64(0), hostMonthlyCount(t, s, token), "Step 3: monthly count should be reset")

	resp = SendWebhook(t, s, SubscriptionCancelled(hostID, "pro-in-v1", "monthly"))
	resp.Body.Close()

	assert.Equal(t, "free", hostTier(t, s, token), "Step 4: tier should be free after cancellation")

	// GetSubscription filters by cancelled_at=nil, so cancelled sub returns 404
	status, _ := subStatus(t, s, token)
	assert.Equal(t, "none", status, "Step 4: cancelled subscription should not be returned by API")
}

// --- Subscription Endpoints ---

func TestBilling_SubscriptionEndpoints(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "Sub Host", "sub-end@test.com", "password")

	t.Run("GET /subscription - no sub returns 404", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("GET /subscription - active sub returns 200", func(t *testing.T) {
		ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")
		resp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("POST /cancel - unauthenticated returns 401", func(t *testing.T) {
		resp, err := util.POST(s, "/api/v1/billing/subscription/cancel", map[string]any{})
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("POST /subscription/update-payment-method", func(t *testing.T) {
		// Setup Mock
		s.DodoMock.Reset()

		reqBody := map[string]any{
			"return_url": "https://queuebuzz.app/settings/billing",
		}
		resp, err := util.POST(s, "/api/v1/billing/subscription/update-payment-method", reqBody, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		data := util.DecodedBody(t, resp)
		assert.Contains(t, data["payment_link"], "dodopayments.com/update")
	})

	t.Run("POST /subscription/cancel with feedback", func(t *testing.T) {
		s.CleanDB()
		token, hostID, _ := authutil.RegisterHost(t, s, "Cancel Sub", "cancel@test.com", "password")
		_ = hostID
		ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

		reqBody := map[string]any{
			"comment":  "Too expensive",
			"feedback": "missing_features",
		}
		resp, err := util.POST(s, "/api/v1/billing/subscription/cancel", reqBody, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Verify cancellation scheduled via API
		status, subData := subStatus(t, s, token)
		assert.Equal(t, "active", status)
		assert.Equal(t, true, subData["cancel_at_period_end"], "subscription should be marked for cancellation")
	})
}

// --- Cache Invalidation ---

func TestBilling_CacheInvalidation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "Cache Host", "cache@test.com", "password")

	// Verify host starts on free tier (populates cache)
	assert.Equal(t, "free", hostTier(t, s, token))

	// Payment webhook should invalidate cache and upgrade tier
	resp := SendWebhook(t, s, PaymentSucceeded(hostID, "pro-in-v1", "monthly"))
	resp.Body.Close()

	// If cache wasn't invalidated, this would still return "free"
	assert.Equal(t, "pro", hostTier(t, s, token), "billing cache should be invalidated after webhook")
}

func TestBilling_GracePeriodExpiry(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	email := fmt.Sprintf("grace-%d@test.com", time.Now().UnixNano())
	token, hostID, _ := authutil.RegisterHost(t, s, "Grace Host", email, "password123")

	// TEST_SETUP: No API to simulate payment failure timing. Directly set grace state.
	now := time.Now()
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")
	_, err := s.DB.Collection("billing_subscriptions").UpdateOne(context.Background(), bson.M{"host_id": hostID}, bson.M{
		"$set": bson.M{
			"status":             "in_grace",
			"grace_period_until": now.Add(1 * time.Hour),
		},
	})
	require.NoError(t, err)

	t.Run("In grace not expired - Pro allowed", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		data := util.DecodedBody(t, resp)
		assert.Equal(t, "pro", data["tier"])
	})

	// TEST_SETUP: Simulate expired grace period — no API to fast-forward time
	_, err = s.DB.Collection("billing_subscriptions").UpdateOne(context.Background(), bson.M{"host_id": hostID}, bson.M{
		"$set": bson.M{
			"status":             "in_grace",
			"grace_period_until": now.Add(-1 * time.Hour),
		},
	})
	require.NoError(t, err)

	// TEST_SETUP: Clear Redis cache to force re-evaluation of grace period
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)

	t.Run("In grace expired - falls back to Free", func(t *testing.T) {
		resp, err := util.GET(s, "/api/v1/billing/subscription", util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		// Check host tier directly (GetMe calls FindByID)
		resp, _ = util.GET(s, "/api/v1/host/me", util.AuthCookie(token))
		data := util.DecodedBody(t, resp)
		assert.Equal(t, "free", data["tier"])
	})
}
