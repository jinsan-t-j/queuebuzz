package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestBilling_RenewalReminders(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	ctx := context.Background()
	hostEmail := "reminder@test.com"
	// 1. Host Registers
	accessToken, hostID, _ := authutil.RegisterHost(t, s, "Reminder Host", hostEmail, "password")

	// 2. Host lists plans (Lifecycle step)
	reqPlans, _ := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/billing/plans?country=IN", nil)
	respPlans, err := s.Do(reqPlans)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, respPlans.StatusCode)
	respPlans.Body.Close()

	// 3. Host creates checkout session (Lifecycle step)
	checkoutPayload, _ := json.Marshal(map[string]any{
		"plan_id":       "pro-in-v1",
		"billing_cycle": "monthly",
	})
	reqCheckout, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/checkout", bytes.NewReader(checkoutPayload))
	reqCheckout.Header.Set("Content-Type", "application/json")
	reqCheckout.AddCookie(util.AuthCookie(accessToken))
	respCheckout, err := s.Do(reqCheckout)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, respCheckout.StatusCode)
	respCheckout.Body.Close()

	// 4. Provision subscription via Webhook (Simulates payment success)
	ProvisionSubscription(t, s, hostID, "pro-in-v1", "monthly")

	// 5. Verify subscription status via API (Lifecycle step)
	reqSub, _ := http.NewRequest(http.MethodGet, s.BaseURL+"/api/v1/billing/subscription", nil)
	reqSub.AddCookie(util.AuthCookie(accessToken))
	respSub, err := s.Do(reqSub)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, respSub.StatusCode)

	var subResp struct {
		Data struct {
			Tier   string `json:"tier"`
			Status string `json:"status"`
		} `json:"data"`
	}
	err = util.DecodeJSON(respSub, &subResp)
	require.NoError(t, err)
	require.Equal(t, "pro", subResp.Data.Tier)
	require.Equal(t, "active", subResp.Data.Status)
	respSub.Body.Close()

	// 6. Manipulate CurrentPeriodEnd to be 2 days from now (within the 3-day window)
	expiryDate := time.Now().Add(48 * time.Hour)
	_, err = s.DB.Collection("billing_subscriptions").UpdateOne(ctx, bson.M{"host_id": hostID}, bson.M{
		"$set": bson.M{
			"current_period_end":       expiryDate,
			"renewal_reminder_sent_at": nil,
		},
	})
	require.NoError(t, err)

	// 7. Run the reminder logic (via the registered Job)
	reminderJob := s.App.Container.Billing.ReminderJob
	err = reminderJob.Run(ctx)
	require.NoError(t, err)

	// 8. Assert email received
	util.AssertEmailReceived(t, hostEmail, "Subscription renews in 2 days")

	// Idempotency: verify the reminder flag was set (no second email should be sent)

	// 6. Run again and verify no second email (idempotency)
	util.ClearMailpit(t)
	err = reminderJob.Run(ctx)
	require.NoError(t, err)

	assertNoEmailReceived(t, hostEmail)
}

func assertNoEmailReceived(t *testing.T, toEmail string) {
	t.Helper()
	mailpitURL := os.Getenv("MAILPIT_API_URL")
	if mailpitURL == "" {
		return
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/search?query=to:%s", mailpitURL, toEmail))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 0, result.Total, "Should not have received a duplicate email")
}
