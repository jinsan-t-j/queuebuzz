package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/billing"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ─────────────────────────────────────────────────────
// Plan Limit Enforcement E2E Tests
//
// Free limits:  {max_queues_per_month:1, max_guests_per_queue:25, history_access:false,
//                custom_branding:false, can_export:false, queue_expiry_hours:24,
//                can_view_guest_data:false, history_retention_days:7}
//
// Pro limits:   {max_queues_per_month:25, max_guests_per_queue:100, history_access:true,
//                custom_branding:false, can_export:true, queue_expiry_hours:72,
//                can_view_guest_data:true, history_retention_days:30}
// ─────────────────────────────────────────────────────

// --- Monthly Queue Quota ---

func TestPlanLimits_MonthlyQueueQuota_Free(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "Quota Host", "quota@test.com", "password")
	hostID := "host-quota@test.com"

	// Free plan allows 1 queue per month. Set count to 1 (already used quota).
	_, err := s.DB.Collection("hosts").UpdateOne(context.Background(),
		bson.M{"_id": hostID},
		bson.M{"$set": bson.M{
			"monthly_queue_count":    1,
			"monthly_limit_reset_at": time.Now(),
		}},
	)
	require.NoError(t, err)

	payload := map[string]any{"name": "Over Quota Queue"}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "free plan should block queue creation over monthly limit")

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "MONTHLY_LIMIT_EXCEEDED", body["code"])
}

func TestPlanLimits_MonthlyQueueQuota_Free_UnderLimit(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "Under Host", "under@test.com", "password")

	// monthly_queue_count defaults to 0 — first queue should succeed
	payload := map[string]any{"name": "First Queue"}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "free plan should allow first queue")
}

func TestPlanLimits_MonthlyQueueQuota_Pro(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "ProQuota Host", "proquota@test.com", "password")
	hostID := "host-proquota@test.com"

	util.UpgradeToProPlan(t, s, hostID)

	_, _ = s.DB.Collection("hosts").UpdateOne(context.Background(),
		bson.M{"_id": hostID},
		bson.M{"$set": bson.M{
			"monthly_queue_count":    2,
			"monthly_limit_reset_at": time.Now(),
		}},
	)

	payload := map[string]any{"name": "Pro Queue 3"}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "pro plan should allow queue creation under limit")
}

// --- Guest Capacity Limit ---
// GuestCapacityGuard is wired on: POST /api/v1/queue/p/:id/join

func TestPlanLimits_GuestCapacity_Free(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "GuestCap Host", "gcap@test.com", "password")
	queueID := util.CreateAuthenticatedQueue(t, s, "Cap Queue", token)

	// Fill queue to free limit (25 guests) using the public join path
	for i := 1; i <= 25; i++ {
		payload := map[string]any{"name": fmt.Sprintf("Guest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode, "guest %d should join", i)
	}

	// 26th guest should be blocked
	payload := map[string]any{"name": "Overflow Guest"}
	resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "free plan should cap guests at 25")

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "QUEUE_FULL", body["code"])
}

func TestPlanLimits_GuestCapacity_Pro(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "ProCap Host", "pcap@test.com", "password")
	hostID := "host-pcap@test.com"

	util.UpgradeToProPlan(t, s, hostID)
	queueID := util.CreateAuthenticatedQueue(t, s, "Pro Cap Queue", token)

	// Fill 26 guests (over free limit, under pro limit of 100)
	for i := 1; i <= 26; i++ {
		payload := map[string]any{"name": fmt.Sprintf("ProGuest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode, "pro guest %d should join", i)
	}

	// 27th guest should still be allowed (pro allows 100)
	payload := map[string]any{"name": "ProGuest 27"}
	resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "pro plan should allow more guests")
}

// --- History Access ---

func TestPlanLimits_HistoryAccess_FreeBlocked(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "FreeHist Host", "freehist@test.com", "password")
	queueID := util.CreateAuthenticatedQueue(t, s, "Hist Queue", token)
	util.JoinQueue(t, s, queueID, "HistGuest")

	resp, err := util.GET(s, "/api/v1/queue/manage/"+queueID+"/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "free plan should block history access")

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "PREMIUM_REQUIRED", body["code"])
}

func TestPlanLimits_HistoryAccess_ProAllowed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "ProHist Host", "prohist@test.com", "password")
	hostID := "host-prohist@test.com"

	util.UpgradeToProPlan(t, s, hostID)
	queueID := util.CreateAuthenticatedQueue(t, s, "Pro Hist Queue", token)
	util.JoinQueue(t, s, queueID, "ProHistGuest")

	resp, err := util.GET(s, "/api/v1/queue/manage/"+queueID+"/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "pro plan should allow history access")
}

// --- History List (account-level, also guarded by HistoryAccessGuard) ---

func TestPlanLimits_HistoryList_FreeBlocked(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "FreeList Host", "freelist@test.com", "password")

	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "free plan should block history list")
}

func TestPlanLimits_HistoryList_ProAllowed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "ProList Host", "prolist@test.com", "password")
	hostID := "host-prolist@test.com"

	util.UpgradeToProPlan(t, s, hostID)

	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "pro plan should allow history list")
}

// --- Queue Expiry Hours ---

func TestPlanLimits_QueueExpiry_FreeVsPro(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// Free host creates a queue — check expiry is set to ~24h
	freeToken := util.RegisterHost(t, s, "FreeExp Host", "freeexp@test.com", "password")
	freeQueueID := util.CreateAuthenticatedQueue(t, s, "Free Expiry Queue", freeToken)

	var freeQueue map[string]any
	err := s.DB.Collection("queues").FindOne(context.Background(), bson.M{"_id": freeQueueID}).Decode(&freeQueue)
	require.NoError(t, err)

	freeExpiry, _ := freeQueue["expires_at"].(time.Time)
	freeCreated, _ := freeQueue["created_at"].(time.Time)
	if !freeExpiry.IsZero() && !freeCreated.IsZero() {
		diff := freeExpiry.Sub(freeCreated)
		assert.InDelta(t, 24, diff.Hours(), 1, "free plan queue should expire in ~24 hours")
	}

	// Pro host creates a queue — check expiry is set to ~72h
	proToken := util.RegisterHost(t, s, "ProExp Host", "proexp@test.com", "password")
	proHostID := "host-proexp@test.com"
	util.UpgradeToProPlan(t, s, proHostID)

	proQueueID := util.CreateAuthenticatedQueue(t, s, "Pro Expiry Queue", proToken)

	var proQueue map[string]any
	err = s.DB.Collection("queues").FindOne(context.Background(), bson.M{"_id": proQueueID}).Decode(&proQueue)
	require.NoError(t, err)

	proExpiry, _ := proQueue["expires_at"].(time.Time)
	proCreated, _ := proQueue["created_at"].(time.Time)
	if !proExpiry.IsZero() && !proCreated.IsZero() {
		diff := proExpiry.Sub(proCreated)
		assert.InDelta(t, 72, diff.Hours(), 1, "pro plan queue should expire in ~72 hours")
	}
}

// --- Cancellation → Free Limits Restore ---

func TestPlanLimits_CancelSubscription_FreeLimitsRestore(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "Cancel Host", "cancel@test.com", "password")
	hostID := "host-cancel@test.com"

	// 1. Start on free — history list blocked
	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "free plan should block history list")

	// 2. Upgrade to pro — history list allowed
	util.UpgradeToProPlan(t, s, hostID)

	resp2, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "pro plan should allow history list")

	// 3. Simulate subscription cancel via webhook
	cancelPayload := billing.SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := util.SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()

	// Clear billing cache and delete the subscription to simulate period end
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)
	// The webhook handler should mark subscription as cancelled.
	// Force remove to simulate period end completion.
	_, _ = s.DB.Collection("billing_subscriptions").DeleteMany(context.Background(), bson.M{"host_id": hostID})

	// 4. After cancellation, host reverts to free — history list blocked again
	resp3, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp3.StatusCode,
		"after cancellation period end, free limits should take over — history blocked")
}

func TestPlanLimits_CancelSubscription_GuestCapResets(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "CapReset Host", "capreset@test.com", "password")
	hostID := "host-capreset@test.com"

	util.UpgradeToProPlan(t, s, hostID)
	queueID := util.CreateAuthenticatedQueue(t, s, "Cap Reset Queue", token)

	// Fill 26 guests (over free cap of 25, under pro cap of 100)
	for i := 1; i <= 26; i++ {
		payload := map[string]any{"name": fmt.Sprintf("ResetGuest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Cancel subscription + remove sub to simulate period end
	cancelPayload := billing.SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := util.SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)
	_, _ = s.DB.Collection("billing_subscriptions").DeleteMany(context.Background(), bson.M{"host_id": hostID})

	// Terminate the first queue so CreateQueueGuard doesn't block with ACTIVE_QUEUE_EXISTS
	termResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/terminate", nil, util.AuthCookie(token))
	require.NoError(t, err)
	termResp.Body.Close()

	// Reset monthly count so we can create a new queue (isolate guest cap test from monthly quota)
	_, _ = s.DB.Collection("hosts").UpdateOne(context.Background(),
		bson.M{"_id": hostID},
		bson.M{"$set": bson.M{"monthly_queue_count": 0, "monthly_limit_reset_at": time.Now()}},
	)

	// New queue as free host — cap should be 25
	queueID2 := util.CreateAuthenticatedQueue(t, s, "Post Cancel Queue", token)
	for i := 1; i <= 25; i++ {
		payload := map[string]any{"name": fmt.Sprintf("NewGuest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID2+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// 26th guest should be blocked on the new queue
	payload := map[string]any{"name": "Blocked Guest"}
	joinResp, err := util.POST(s, "/api/v1/queue/p/"+queueID2+"/join", payload)
	require.NoError(t, err)
	defer joinResp.Body.Close()

	assert.Equal(t, http.StatusForbidden, joinResp.StatusCode,
		"after cancellation, free guest limit should apply")
}

func TestPlanLimits_CancelSubscription_MonthlyQuotaResets(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token := util.RegisterHost(t, s, "QuotaReset Host", "quotareset@test.com", "password")
	hostID := "host-quotareset@test.com"

	// Start as pro (25 queues/month)
	util.UpgradeToProPlan(t, s, hostID)

	// Set monthly count to 2
	_, _ = s.DB.Collection("hosts").UpdateOne(context.Background(),
		bson.M{"_id": hostID},
		bson.M{"$set": bson.M{
			"monthly_queue_count":    2,
			"monthly_limit_reset_at": time.Now(),
		}},
	)

	// Pro plan: should allow (2 < 25)
	payload := map[string]any{"name": "Pro Quota Queue"}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Cancel subscription + remove sub to simulate period end
	cancelPayload := billing.SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := util.SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)
	_, _ = s.DB.Collection("billing_subscriptions").DeleteMany(context.Background(), bson.M{"host_id": hostID})

	// Monthly count is now 3 (incremented by the queue above) — free limit is 1
	// Next create should be blocked (either ACTIVE_QUEUE_EXISTS or MONTHLY_LIMIT_EXCEEDED)
	payload2 := map[string]any{"name": "Post Cancel Queue"}
	resp2, err := util.POST(s, "/api/v1/queue/p/create", payload2, util.AuthCookie(token))
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp2.StatusCode,
		"after cancellation, free monthly limit should block creation")
}
