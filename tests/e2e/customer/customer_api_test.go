package customer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomer_Join_HappyPath(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Join Test Queue")

	// Guest joins
	guestToken := qutil.JoinQueue(t, s, queueID, joinCode, "Rajan Kumar")
	assert.NotEmpty(t, guestToken)

	// Guest can view public status
	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/p/%s/live", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCustomer_ConcurrentJoins(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Concurrent Queue")

	const n = 20 // Reduced for speed in E2E, 50 was before
	var wg sync.WaitGroup
	wg.Add(n)

	codes := make([]int, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{
				"display_name": fmt.Sprintf("User%d", idx),
				"fingerprint":  fmt.Sprintf("fp-user-%d", idx),
				"join_code":    joinCode,
			})
			req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			// Each joiner has its own IP to stay within individual rate limits (5/min)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", idx+1))

			resp, err := s.Do(req)
			if err == nil {
				codes[idx] = resp.StatusCode
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()

	successes := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			successes++
		}
	}
	assert.Equal(t, n, successes, "all %d concurrent joins must succeed", n)
}

func TestCustomer_PublicStatus_NotFound(t *testing.T) {
	s := Suite(t)

	resp, err := util.GET(s, "/api/v1/queue/p/non-existent-id/live")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCustomer_Join_PausedQueue(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create a queue
	queueID, joinCode, hostToken := qutil.CreateQueue(t, s, "Paused Test Queue")

	// 2. Pause the queue via Host API
	resp, err := util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/pause", queueID), nil, util.HostCookie(hostToken))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. Try to join as customer - should fail with 403 Forbidden and QUEUE_PAUSED error code
	payload, _ := json.Marshal(map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
	})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var errBody map[string]any
	require.NoError(t, util.DecodeJSON(resp, &errBody))
	assert.Equal(t, "QUEUE_PAUSED", errBody["code"])
}
