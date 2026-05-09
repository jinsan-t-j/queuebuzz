package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	qutil "queuebuzz/tests/e2e/queue/utils"
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

	token, _, _ := authutil.RegisterHost(t, s, "Quota Host", "quota@test.com", "password")

	q1 := qutil.CreateAuthenticatedQueue(t, s, "First Queue", token)
	qutil.TerminateQueue(t, s, q1, token)

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

	token, _, _ := authutil.RegisterHost(t, s, "Under Host", "under@test.com", "password")

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

	token, hostID, _ := authutil.RegisterHost(t, s, "ProQuota Host", "proquota@test.com", "password")

	// Full lifecycle: Register -> Upgrade -> Create multiple queues
	UpgradeToProPlan(t, s, hostID, token)

	// Pro plan allows 25 queues per month. Create 2 queues via API.
	q1 := qutil.CreateAuthenticatedQueue(t, s, "Queue 1", token)
	qutil.TerminateQueue(t, s, q1, token)
	q2 := qutil.CreateAuthenticatedQueue(t, s, "Queue 2", token)
	qutil.TerminateQueue(t, s, q2, token)

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

	token, _, _ := authutil.RegisterHost(t, s, "GuestCap Host", "gcap@test.com", "password")
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Cap Queue", token)

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

	token, hostID, _ := authutil.RegisterHost(t, s, "ProCap Host", "pcap@test.com", "password")

	UpgradeToProPlan(t, s, hostID, token)
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Pro Cap Queue", token)

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

	token, _, _ := authutil.RegisterHost(t, s, "FreeHist Host", "freehist@test.com", "password")
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Hist Queue", token)
	qutil.JoinQueue(t, s, queueID, "HistGuest")

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

	token, hostID, _ := authutil.RegisterHost(t, s, "ProHist Host", "prohist@test.com", "password")

	UpgradeToProPlan(t, s, hostID, token)
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Pro Hist Queue", token)
	qutil.JoinQueue(t, s, queueID, "ProHistGuest")

	resp, err := util.GET(s, "/api/v1/queue/manage/"+queueID+"/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "pro plan should allow history access")
}

// --- History List (account-level, also guarded by HistoryAccessGuard) ---

func TestPlanLimits_HistoryList_FreeAllowed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "FreeList Host", "freelist@test.com", "password")

	// Free plan should ALLOW history list (new policy)
	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "free plan should allow history list")
}

func TestPlanLimits_HistoryList_ProAllowed(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "ProList Host", "prolist@test.com", "password")

	UpgradeToProPlan(t, s, hostID, token)

	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "pro plan should allow history list")
}

// --- Queue Expiry Hours ---

func TestPlanLimits_QueueExpiry_FreeVsPro(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// Free host creates a queue — verify expiry is ~24h from API response
	freeToken, _, _ := authutil.RegisterHost(t, s, "FreeExp Host", "freeexp@test.com", "password")
	payload := map[string]any{"name": "Free Expiry Queue"}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(freeToken))
	require.NoError(t, err)
	var freeBody map[string]any
	require.NoError(t, util.DecodeJSON(resp, &freeBody))
	resp.Body.Close()

	freeData := freeBody["data"].(map[string]any)
	freeCreated, _ := time.Parse(time.RFC3339, freeData["created_at"].(string))
	freeExpiry, _ := time.Parse(time.RFC3339, freeData["expires_at"].(string))
	if !freeExpiry.IsZero() && !freeCreated.IsZero() {
		diff := freeExpiry.Sub(freeCreated)
		assert.InDelta(t, 24, diff.Hours(), 1, "free plan queue should expire in ~24 hours")
	}

	// Pro host creates a queue — verify expiry is ~72h from API response
	proToken, hostID, _ := authutil.RegisterHost(t, s, "ProExp Host", "proexp@test.com", "password")
	UpgradeToProPlan(t, s, hostID, proToken)

	payload2 := map[string]any{"name": "Pro Expiry Queue"}
	resp2, err := util.POST(s, "/api/v1/queue/p/create", payload2, util.AuthCookie(proToken))
	require.NoError(t, err)
	var proBody map[string]any
	require.NoError(t, util.DecodeJSON(resp2, &proBody))
	resp2.Body.Close()

	proData := proBody["data"].(map[string]any)
	proCreated, _ := time.Parse(time.RFC3339, proData["created_at"].(string))
	proExpiry, _ := time.Parse(time.RFC3339, proData["expires_at"].(string))
	if !proExpiry.IsZero() && !proCreated.IsZero() {
		diff := proExpiry.Sub(proCreated)
		assert.InDelta(t, 72, diff.Hours(), 1, "pro plan queue should expire in ~72 hours")
	}
}

// --- Cancellation → Free Limits Restore ---

func TestPlanLimits_CancelSubscription_FreeLimitsRestore(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "Basic Host", "basic@test.com", "password")

	// 1. Start on free — history list allowed (new policy)
	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "free plan should allow history list")

	// 2. Upgrade to pro — history list allowed
	UpgradeToProPlan(t, s, hostID, token)

	resp2, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "pro plan should allow history list")

	// 3. Simulate subscription cancel via webhook
	// Simulate full cancellation lifecycle via webhook
	cancelPayload := SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()

	// 4. After cancellation, host reverts to free
	// History list should still be allowed
	resp3, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, http.StatusOK, resp3.StatusCode, "after cancellation, list should still be allowed")

	// History detail should be blocked
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Test Queue", token)
	resp4, err := util.GET(s, "/api/v1/queue/manage/"+queueID+"/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp4.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp4.StatusCode, "after cancellation, detail should be blocked")
}

func TestPlanLimits_CancelSubscription_GuestCapResets(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "CapReset Host", "capreset@test.com", "password")

	UpgradeToProPlan(t, s, hostID, token)
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Cap Reset Queue", token)

	// Fill 26 guests (over free cap of 25, under pro cap of 100)
	for i := 1; i <= 26; i++ {
		payload := map[string]any{"name": fmt.Sprintf("ResetGuest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Cancel subscription + remove sub to simulate period end
	// Cancel subscription via webhook
	cancelPayload := SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()

	// Terminate the first queue so CreateQueueGuard doesn't block with ACTIVE_QUEUE_EXISTS
	termResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/terminate", nil, util.AuthCookie(token))
	require.NoError(t, err)
	termResp.Body.Close()

	// TEST_SETUP: Reset monthly count — no admin API to reset billing counters
	_, _ = s.DB.Collection("hosts").UpdateOne(context.Background(),
		bson.M{"_id": hostID},
		bson.M{"$set": bson.M{"monthly_queue_count": 0, "monthly_limit_reset_at": time.Now()}},
	)
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)

	// New queue as free host — cap should be 25
	queueID2 := qutil.CreateAuthenticatedQueue(t, s, "Post Cancel Queue", token)
	for i := 1; i <= 25; i++ {
		payload := map[string]any{"name": fmt.Sprintf("NewGuest %d", i)}
		resp, err := util.POST(s, "/api/v1/queue/p/"+queueID2+"/join", payload)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// 26th guest should be blocked on the new queue
	payload := map[string]any{"name": "Blocked Guest"}
	// after cancellation, free guest limit should apply
	var joinResp *http.Response
	for i := 0; i < 10; i++ {
		joinResp, err = util.POST(s, "/api/v1/queue/p/"+queueID2+"/join", payload)
		if err == nil && joinResp.StatusCode == http.StatusForbidden {
			break
		}
		if joinResp != nil {
			joinResp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, err)
	defer joinResp.Body.Close()

	assert.Equal(t, http.StatusForbidden, joinResp.StatusCode,
		"after cancellation, free guest limit should apply")
}

func TestPlanLimits_CancelSubscription_MonthlyQuotaResets(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "QuotaReset Host", "quotareset@test.com", "password")

	// Start as pro (25 queues/month)
	UpgradeToProPlan(t, s, hostID, token)

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
	// Cancel subscription via webhook
	cancelPayload := SubscriptionCancelled(hostID, "pro-in-v1", "monthly")
	webhookResp := SendWebhook(t, s, cancelPayload)
	webhookResp.Body.Close()

	// Monthly count is now 3 (incremented by the queue above) — free limit is 1
	// Next create should be blocked (either ACTIVE_QUEUE_EXISTS or MONTHLY_LIMIT_EXCEEDED)
	payload2 := map[string]any{"name": "Post Cancel Queue"}
	// Next create should be blocked
	var resp2 *http.Response
	for i := 0; i < 10; i++ {
		resp2, err = util.POST(s, "/api/v1/queue/p/create", payload2, util.AuthCookie(token))
		if err == nil && resp2.StatusCode == http.StatusForbidden {
			break
		}
		if resp2 != nil {
			resp2.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp2.StatusCode,
		"after cancellation, free monthly limit should block creation")
}

// --- History Retention ---

func TestPlanLimits_HistoryRetention(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, hostPublicID := authutil.RegisterHost(t, s, "Retention Host", "retention@test.com", "password")
	// Removed manual hostPublicID override to keep token/DB in sync

	// Create two queues: one recent, one old (10 days ago)
	now := time.Now()
	oldDate := now.AddDate(0, 0, -10)

	// Recent Queue
	q1ID := "q-recent"
	_, _ = s.DB.Collection("queues").InsertOne(context.Background(), bson.M{
		"_id": q1ID, "host_id": hostID, "host_public_id": hostPublicID,
		"name": "Recent Queue", "status": "CLOSED",
		"created_at": now.Add(-1 * time.Hour), "updated_at": now.Add(-1 * time.Hour),
	})

	// Old Queue (should be hidden on Free plan which has 7 days retention)
	q2ID := "q-old"
	_, _ = s.DB.Collection("queues").InsertOne(context.Background(), bson.M{
		"_id": q2ID, "host_id": hostID, "host_public_id": hostPublicID,
		"name": "Old Queue", "status": "CLOSED", "created_at": oldDate, "updated_at": oldDate,
	})

	// 1. As Free host (7 days retention) — only 1 queue should show up
	// Note: We need to bypass the HistoryAccessGuard for this specific test
	// or upgrade to a plan that allows history but has limited retention.
	// Actually, Pro has 30 days retention. Let's create a custom plan or just tweak the Pro plan in DB for this test.

	// Let's just use Pro plan but set retention to 5 days for testing
	UpgradeToProPlan(t, s, hostID, token)
	_, _ = s.DB.Collection("billing_plans").UpdateOne(context.Background(),
		bson.M{"_id": "pro-in-v1"},
		bson.M{"$set": bson.M{"limits.history_retention_days": 5}},
	)
	// IMPORTANT: Clear plan cache so new retention is used
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)
	// Clear cache
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)

	resp, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, util.DecodeJSON(resp, &body))
	historyData := body["data"].(map[string]any)
	if historyData["data"] == nil {
		assert.Equal(t, 0, 1, "history data should not be nil")
		return
	}
	data := historyData["data"].([]any)

	assert.Equal(t, 1, len(data), "only the recent queue should be visible due to retention limit")
	assert.Equal(t, "Recent Queue", data[0].(map[string]any)["name"])

	// 2. Increase retention to 15 days — both should show up
	_, _ = s.DB.Collection("billing_plans").UpdateOne(context.Background(),
		bson.M{"_id": "pro-in-v1"},
		bson.M{"$set": bson.M{"limits.history_retention_days": 15}},
	)
	s.App.Container.Redis.Del(context.Background(), "billing:host_plan:"+hostID)

	// Also clear the cached history list from the first request
	keys, _ := s.App.Container.Redis.Keys(context.Background(), "host:"+hostID+":history:*").Result()
	if len(keys) > 0 {
		s.App.Container.Redis.Del(context.Background(), keys...)
	}

	resp2, err := util.GET(s, "/api/v1/queue/history", util.AuthCookie(token))
	require.NoError(t, err)
	defer resp2.Body.Close()

	var body2 map[string]any
	require.NoError(t, util.DecodeJSON(resp2, &body2))
	historyData2 := body2["data"].(map[string]any)
	data2 := historyData2["data"].([]any)
	assert.Equal(t, 2, len(data2), "both queues should be visible after increasing retention")
}
