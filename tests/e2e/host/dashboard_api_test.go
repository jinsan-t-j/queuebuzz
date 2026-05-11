package host

import (
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboard_EmptyState(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// Register a new host to get a clean state
	email := fmt.Sprintf("empty-%d@example.com", time.Now().UnixNano())
	hostToken, _, _ := authutil.RegisterHost(t, s, "Empty Host", email, "password123")

	// Fetch dashboard
	resp, err := util.GET(s, "/api/v1/queue/dashboard", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)

	// Verify empty stats
	stats := data["stats"].(map[string]any)
	assert.Equal(t, float64(0), stats["servedToday"])
	assert.Equal(t, "0m", stats["avgWait"])

	// Verify QuickSetup is shown
	quickSetup := data["quickSetup"].(map[string]any)
	assert.True(t, quickSetup["show"].(bool))
	steps := quickSetup["steps"].([]any)
	for _, step := range steps {
		assert.False(t, step.(map[string]any)["isDone"].(bool))
	}

	// Verify ActiveQueue is not active
	activeQueue := data["activeQueue"].(map[string]any)
	assert.False(t, activeQueue["isActive"].(bool))
}

func TestDashboard_WithActiveQueue(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create a host and a queue
	email := fmt.Sprintf("active-%d@example.com", time.Now().UnixNano())
	hostToken, _, _ := authutil.RegisterHost(t, s, "Active Host", email, "password123")

	// Use the host token to create a queue (optional, but good for linking)
	// Wait, CreateQueue in util.go uses anonymous creation.
	// Let's create a queue using the host token.
	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Active Dashboard Queue", hostToken)

	// 2. Fetch dashboard
	resp, err := util.GET(s, "/api/v1/queue/dashboard", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)

	// 3. Verify ActiveQueue is active
	activeQueue := data["activeQueue"].(map[string]any)
	assert.True(t, activeQueue["isActive"].(bool))
	assert.Equal(t, "Active Dashboard Queue", activeQueue["queueName"])

	// 4. Join a customer and serve them
	qutil.JoinQueue(t, s, queueID, joinCode, "Customer 1")
	qutil.ServeNext(t, s, queueID, hostToken)

	// 5. Fetch dashboard again
	resp, err = util.GET(s, "/api/v1/queue/dashboard", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()

	data = util.DecodedBody(t, resp)
	stats := data["stats"].(map[string]any)
	assert.Equal(t, float64(1), stats["servedToday"])
}

func TestHistory_List_And_Summary(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create a host and a historical queue
	email := fmt.Sprintf("history-%d@example.com", time.Now().UnixNano())
	hostToken, hostID, _ := authutil.RegisterHost(t, s, "History Host", email, "password123")
	billing.UpgradeToPremium(t, s, hostID, hostToken)

	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Historical Queue", hostToken)

	qutil.JoinQueue(t, s, queueID, joinCode, "Guest")
	qutil.ServeNext(t, s, queueID, hostToken)

	// Terminate queue to make it historical
	resp, err := util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/terminate", queueID), nil, util.AuthCookie(hostToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Fetch history list
	resp, err = util.GET(s, "/api/v1/queue/history", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	historyData := util.DecodedBody(t, resp)

	items := historyData["data"].([]any)
	assert.Len(t, items, 1)
	assert.Equal(t, "Historical Queue", items[0].(map[string]any)["name"])

	summary := historyData["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["total_sessions"])
	assert.Equal(t, float64(1), summary["total_served"])
}

func TestHistory_Pagination_And_Filtering(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	email := fmt.Sprintf("multi-%d@example.com", time.Now().UnixNano())
	hostToken, hostID, _ := authutil.RegisterHost(t, s, "Multi Host", email, "password123")
	billing.UpgradeToPremium(t, s, hostID, hostToken)

	// Create 3 historical queues
	queues := []string{"Queue Alpha", "Queue Beta", "Queue Gamma"}
	for _, name := range queues {
		qid, _ := qutil.CreateAuthenticatedQueue(t, s, name, hostToken)
		util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/terminate", qid), nil, util.AuthCookie(hostToken))
	}

	// 1. Test Limit (Page 1, Limit 2)
	resp, _ := util.GET(s, "/api/v1/queue/history?limit=2&page=1", util.AuthCookie(hostToken))
	data := util.DecodedBody(t, resp)
	items := data["data"].([]any)
	assert.Len(t, items, 2)
	assert.Equal(t, float64(3), data["total_count"])
	assert.Equal(t, float64(2), data["total_pages"])

	// 2. Test Search (Search "Beta")
	resp, _ = util.GET(s, "/api/v1/queue/history?search=Beta", util.AuthCookie(hostToken))
	data = util.DecodedBody(t, resp)
	items = data["data"].([]any)
	assert.Len(t, items, 1)
	assert.Equal(t, "Queue Beta", items[0].(map[string]any)["name"])

	// 3. Test Search (Search "Queue")
	resp, _ = util.GET(s, "/api/v1/queue/history?search=Queue", util.AuthCookie(hostToken))
	data = util.DecodedBody(t, resp)
	items = data["data"].([]any)
	assert.Len(t, items, 3)
}
