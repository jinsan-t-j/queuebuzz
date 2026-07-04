package customer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
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

func TestCustomer_Join_DuplicatePhone(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Phone Dup Queue")

	// Join first customer with a phone number
	payload1 := map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
		"phone":        "9876543210",
	}
	body1, _ := json.Marshal(payload1)
	req1, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := s.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, http.StatusCreated, resp1.StatusCode)

	// Try joining second customer with the SAME phone number
	payload2 := map[string]any{
		"display_name": "Rahul Kumar",
		"fingerprint":  "fp-rahul",
		"join_code":    joinCode,
		"phone":        "9876543210",
	}
	body2, _ := json.Marshal(payload2)
	req2, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := s.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()

	// FieldException/Duplicate exception returns 429 Status Too Many Requests in Fiber Handler
	assert.Equal(t, http.StatusTooManyRequests, resp2.StatusCode)
}

func TestCustomer_PhoneValidation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Phone Validation Queue")

	// 1. Phone number too short (<8 characters)
	payload1 := map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
		"phone":        "1234567",
	}
	body1, _ := json.Marshal(payload1)
	req1, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := s.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp1.StatusCode)

	// 2. Phone number too long (>15 characters)
	payload2 := map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
		"phone":        "1234567890123456",
	}
	body2, _ := json.Marshal(payload2)
	req2, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := s.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)

	// 3. Valid phone number (9 characters)
	payload3 := map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
		"phone":        "987654321",
	}
	body3, _ := json.Marshal(payload3)
	req3, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := s.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, http.StatusCreated, resp3.StatusCode)
}

func TestCustomer_UpdatePhone(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Update Phone Queue")

	// Join queue without phone
	payload := map[string]any{
		"display_name": "Rajan Kumar",
		"fingerprint":  "fp-rajan",
		"join_code":    joinCode,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	entryID := data["id"].(string)
	guestToken := util.ExtractCookie(t, resp, "guest_entry_token", true)
	resp.Body.Close()

	// Update phone number
	updatePayload := map[string]any{
		"phone": "9876543210",
	}
	respUpdate, err := util.POST(s, "/api/v1/customer/entry/update", updatePayload, util.GuestCookie(guestToken))
	require.NoError(t, err)
	respUpdate.Body.Close()
	assert.Equal(t, http.StatusOK, respUpdate.StatusCode)

	// Verify phone number in MongoDB
	var entry bson.M
	err = s.DB.Collection("queue_entries").FindOne(context.Background(), bson.M{"_id": entryID}).Decode(&entry)
	require.NoError(t, err)
	assert.Equal(t, "9876543210", entry["phone"])
}
