package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/util"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomer_Join_HappyPath(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _ := util.CreateQueue(t, s, "Join Test Queue")

	// Guest joins
	guestToken := util.JoinQueue(t, s, queueID, "Rajan Kumar")
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

	queueID, _ := util.CreateQueue(t, s, "Concurrent Queue")

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
