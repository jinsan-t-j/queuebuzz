package queue

import (
	"context"
	"fmt"
	authutil "queuebuzz/tests/e2e/auth/utils"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestQueue_Expiry(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	ctx := context.Background()
	token, hostID, _ := authutil.RegisterHost(t, s, "Expiry Host", "expiry-test@test.com", "password")

	// Helper to get monthly count from DB
	getCount := func() int {
		var host struct {
			MonthlyQueueCount int `bson:"monthly_queue_count"`
		}
		err := s.DB.Collection("hosts").FindOne(ctx, bson.M{"_id": hostID}).Decode(&host)
		require.NoError(t, err)
		return host.MonthlyQueueCount
	}

	// 1. Create a queue
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Expirable Queue", token)
	assert.Equal(t, 1, getCount(), "Quota should be 1 after creation")

	// 2. Add some entries (to verify cleanup)
	qutil.JoinQueue(t, s, queueID, "Customer 1")
	qutil.JoinQueue(t, s, queueID, "Customer 2")

	// Verify Redis keys exist
	exists, _ := s.App.Container.Redis.Exists(ctx, fmt.Sprintf("queue_positions:%s", queueID)).Result()
	assert.Equal(t, int64(1), exists, "Redis positions key should exist after joining")

	// 2.5 Populate history list and summary caches via API
	resp, err := util.GET(s, "/api/v1/queue/history?filter=all", util.AuthCookie(token))
	require.NoError(t, err)
	resp.Body.Close()

	// Verify Caches EXIST before expiry
	historyKey := fmt.Sprintf("host:%s:history:list::all:1:10", hostID)
	summaryKey := fmt.Sprintf("host:%s:history:summary", hostID)
	exists, _ = s.App.Container.Redis.Exists(ctx, historyKey).Result()
	assert.Equal(t, int64(1), exists, "History list cache should exist after API call")
	exists, _ = s.App.Container.Redis.Exists(ctx, summaryKey).Result()
	assert.Equal(t, int64(1), exists, "History summary cache should exist after API call")

	// 3. Force expire the queue in DB
	_, err = s.DB.Collection("queues").UpdateOne(ctx,
		bson.M{"_id": queueID},
		bson.M{"$set": bson.M{"expires_at": time.Now().Add(-1 * time.Hour)}},
	)
	require.NoError(t, err)

	// 4. Trigger sweep manually using the job pattern
	err = s.App.Container.Queue.ExpiryJob.Run(ctx)
	require.NoError(t, err)

	// 5. Verify status is EXPIRED
	var queueDoc bson.M
	err = s.DB.Collection("queues").FindOne(ctx, bson.M{"_id": queueID}).Decode(&queueDoc)
	require.NoError(t, err)
	assert.Equal(t, "EXPIRED", queueDoc["status"])

	// 6. Verify Redis keys are cleaned up
	exists, _ = s.App.Container.Redis.Exists(ctx, fmt.Sprintf("queue_positions:%s", queueID)).Result()
	assert.Equal(t, int64(0), exists, "Redis positions key should be deleted after expiry")

	// 6.5 Verify History Caches are invalidated
	exists, _ = s.App.Container.Redis.Exists(ctx, historyKey).Result()
	assert.Equal(t, int64(0), exists, "History list cache should be invalidated")
	exists, _ = s.App.Container.Redis.Exists(ctx, summaryKey).Result()
	assert.Equal(t, int64(0), exists, "History summary cache should be invalidated")

	// 7. Verify Quota is STILL 1 (per business policy: expiry != deletion)
	assert.Equal(t, 1, getCount(), "Quota should still be 1 after expiry")
}
