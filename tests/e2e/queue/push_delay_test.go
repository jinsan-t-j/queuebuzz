package queue

import (
	"fmt"
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue_BufferMins_PushDelay(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create a queue
	queueID, joinCode, hostToken := qutil.CreateQueue(t, s, "Push Delay Queue")

	// 2. Set buffer_mins via PATCH request
	payload := map[string]any{
		"buffer_mins": 10,
	}
	resp, err := util.PATCH(s, fmt.Sprintf("/api/v1/queue/manage/%s", queueID), payload, util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Verify buffer_mins is updated in public queue live info
	resp, err = util.GET(s, fmt.Sprintf("/api/v1/queue/p/%s/live", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	assert.InDelta(t, 10, data["buffer_mins"].(float64), 1.0)

	// 4. Join a guest
	qutil.JoinQueue(t, s, queueID, joinCode, "Waiting Guest")

	// 5. Call next guest (which should reset buffer_mins to 0)
	resp, err = util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/call", queueID), map[string]any{"next": true}, util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 6. Verify buffer_mins is now 0 in the public queue status
	resp, err = util.GET(s, fmt.Sprintf("/api/v1/queue/p/%s/live", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data = util.DecodedBody(t, resp)
	assert.Equal(t, float64(0), data["buffer_mins"].(float64))

	// 7. Test setting buffer_mins back to 0 explicitly
	payload = map[string]any{
		"buffer_mins": 0,
	}
	resp, err = util.PATCH(s, fmt.Sprintf("/api/v1/queue/manage/%s", queueID), payload, util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
