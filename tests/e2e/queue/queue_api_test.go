package queue

import (
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue_Management_Lifecycle(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, hostID, _ := authutil.RegisterHost(t, s, "Host", "host@test.com", "password")
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Lifecycle Queue", token)
	billing.UpgradeToPremium(t, s, hostID, token)

	// 1. Join some customers
	qutil.JoinQueue(t, s, queueID, "Customer 1")
	qutil.JoinQueue(t, s, queueID, "Customer 2")

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

	_, hostToken1 := qutil.CreateQueue(t, s, "Queue A")
	queueBID, _ := qutil.CreateQueue(t, s, "Queue B")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueBID), util.HostCookie(hostToken1))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestQueue_Management_Unauthorized(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _ := qutil.CreateQueue(t, s, "No Auth Queue")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestQueue_Settings_Update(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, hostToken := qutil.CreateQueue(t, s, "Settings Queue")

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
