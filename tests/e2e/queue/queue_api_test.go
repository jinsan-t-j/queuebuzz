package queue

import (
	"context"
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestQueue_Management_Lifecycle(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "Host", "host@test.com", "password")
	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Lifecycle Queue", token)
	billing.UpgradeToPremium(t, s, hostID, token)

	// 1. Join some customers
	qutil.JoinQueue(t, s, queueID, joinCode, "Customer 1")
	qutil.JoinQueue(t, s, queueID, joinCode, "Customer 2")

	// 2. Fetch history
	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueID), util.HostCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Pause queue
	resp, err = util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/pause", queueID), nil, util.HostCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 4. Resume queue
	resp, err = util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/resume", queueID), nil, util.HostCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestQueue_Management_CrossOwner_Forbidden(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	_, _, hostToken1 := qutil.CreateQueue(t, s, "Queue A")
	queueBID, _, _ := qutil.CreateQueue(t, s, "Queue B")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueBID), util.HostCookie(hostToken1))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestQueue_Management_Unauthorized(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _, _ := qutil.CreateQueue(t, s, "No Auth Queue")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestQueue_Settings_Update(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _, hostToken := qutil.CreateQueue(t, s, "Settings Queue")

	// 1. Update settings
	strictMode := true
	collectEmails := true
	notes := "Welcome to our store!"
	payload := map[string]any{
		"strict_queue_mode": strictMode,
		"collect_emails":    collectEmails,
		"notes":             notes,
		"avg_service_mins":  15,
	}

	resp, err := util.PATCH(s, fmt.Sprintf("/api/v1/queue/manage/%s", queueID), payload, util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Verify settings are applied (via management detail/history)
	// Note: GetLiveQueueByID returns the queue record
	resp, err = util.GET(s, fmt.Sprintf("/api/v1/queue/p/%s/live", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()

	data := util.DecodedBody(t, resp)
	assert.Equal(t, notes, data["notes"])
	assert.Equal(t, float64(15), data["avg_service_mins"])
}

func TestQueue_Delete(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	ctx := context.Background()
	token, hostID, _ := authutil.RegisterHost(t, s, "Delete Host", "delete-test@test.com", "password")

	getCount := func() int {
		var host struct {
			MonthlyQueueCount int `bson:"monthly_queue_count"`
		}
		err := s.DB.Collection("hosts").FindOne(ctx, bson.M{"_id": hostID}).Decode(&host)
		require.NoError(t, err)
		return host.MonthlyQueueCount
	}

	// 1. Create a queue
	queueID, _ := qutil.CreateAuthenticatedQueue(t, s, "Deletable Queue", token)
	assert.Equal(t, 1, getCount())

	// 2. Delete via API
	resp, err := util.DELETE(s, "/api/v1/queue/history/"+queueID, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify quota decremented
	assert.Equal(t, 0, getCount(), "Quota should be 0 after deletion")
}

func TestQueue_BulkDelete(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	ctx := context.Background()
	token, hostID, _ := authutil.RegisterHost(t, s, "Bulk Host", "bulk-test@test.com", "password")

	// 0. Upgrade to Pro plan (manually for test setup) to allow >1 queue
	var proPlan struct {
		ID string `bson:"_id"`
	}
	err := s.DB.Collection("billing_plans").FindOne(ctx, bson.M{"tier": "pro"}).Decode(&proPlan)
	require.NoError(t, err)

	_, err = s.DB.Collection("billing_subscriptions").InsertOne(ctx, bson.M{
		"_id":                uuid.New().String(),
		"host_id":            hostID,
		"plan_id":            proPlan.ID,
		"status":             "active",
		"current_period_end": time.Now().Add(30 * 24 * time.Hour),
		"created_at":         time.Now(),
		"updated_at":         time.Now(),
	})
	require.NoError(t, err)

	// Also update host tier just in case
	_, err = s.DB.Collection("hosts").UpdateOne(ctx, bson.M{"_id": hostID}, bson.M{"$set": bson.M{"tier": "pro"}})
	require.NoError(t, err)

	// Invalidate billing cache so the new plan is picked up
	_ = s.App.Container.Redis.Del(ctx, "billing:host_plan:"+hostID).Err()

	getCount := func() int {
		var host struct {
			MonthlyQueueCount int `bson:"monthly_queue_count"`
		}
		err := s.DB.Collection("hosts").FindOne(ctx, bson.M{"_id": hostID}).Decode(&host)
		require.NoError(t, err)
		return host.MonthlyQueueCount
	}

	// 1. Create multiple queues (one at a time, terminating each to allow next creation)
	q1, _ := qutil.CreateAuthenticatedQueue(t, s, "Queue 1", token)
	resp, err := util.POST(s, "/api/v1/queue/manage/"+q1+"/terminate", nil, util.AuthCookie(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	q2, _ := qutil.CreateAuthenticatedQueue(t, s, "Queue 2", token)
	resp, err = util.POST(s, "/api/v1/queue/manage/"+q2+"/terminate", nil, util.AuthCookie(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	q3, _ := qutil.CreateAuthenticatedQueue(t, s, "Queue 3", token)
	resp, err = util.POST(s, "/api/v1/queue/manage/"+q3+"/terminate", nil, util.AuthCookie(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	assert.Equal(t, 3, getCount())

	// 2. Bulk delete two of them
	payload := map[string][]string{"ids": {q1, q2}}
	resp, err = util.DELETEWithBody(s, "/api/v1/queue/history", payload, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify quota decremented by 2
	assert.Equal(t, 1, getCount(), "Quota should be 1 after bulk deleting 2 of 3 queues")

	// 4. Delete the last one
	resp, err = util.DELETE(s, "/api/v1/queue/history/"+q3, util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 0, getCount(), "Quota should be 0 after deleting last queue")
}

func TestQueue_Terminate_InvalidatesCache(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	ctx := context.Background()
	token, hostID, _ := authutil.RegisterHost(t, s, "Terminator", "terminate@test.com", "password")
	queueID, _ := qutil.CreateAuthenticatedQueue(t, s, "Terminatable Queue", token)

	// 1. Populate history and summary caches via API
	resp, err := util.GET(s, "/api/v1/queue/history?filter=all", util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()

	// Verify Caches EXIST before termination
	historyKey := fmt.Sprintf("host:%s:history:list::all:1:10", hostID)
	summaryKey := fmt.Sprintf("host:%s:history:summary", hostID)
	exists, _ := s.App.Container.Redis.Exists(ctx, historyKey).Result()
	assert.Equal(t, int64(1), exists, "History list cache should exist after API call")
	exists, _ = s.App.Container.Redis.Exists(ctx, summaryKey).Result()
	assert.Equal(t, int64(1), exists, "History summary cache should exist after API call")

	// 2. Terminate queue
	resp, err = util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/terminate", queueID), nil, util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify caches are invalidated
	exists, _ = s.App.Container.Redis.Exists(ctx, historyKey).Result()
	assert.Equal(t, int64(0), exists, "History list cache should be invalidated after termination")
	exists, _ = s.App.Container.Redis.Exists(ctx, summaryKey).Result()
	assert.Equal(t, int64(0), exists, "History summary cache should be invalidated after termination")
}
