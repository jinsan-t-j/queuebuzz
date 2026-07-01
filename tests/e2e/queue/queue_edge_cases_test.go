package queue

import (
	"bytes"
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

func TestQueue_StrictMode(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "Strict Host", "strict@test.com", "password")

	// Create queue first
	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Strict Queue", token)

	// Enable strict mode via PATCH
	patchPayload := map[string]any{"strict_queue_mode": true}
	patchResp, err := util.PATCH(s, "/api/v1/queue/manage/"+queueID+"/", patchPayload, util.AuthCookie(token))
	require.NoError(t, err)
	patchResp.Body.Close()
	assert.Equal(t, http.StatusOK, patchResp.StatusCode)

	// Add 2 guests
	qutil.JoinQueue(t, s, queueID, joinCode, "Guest 1")
	qutil.JoinQueue(t, s, queueID, joinCode, "Guest 2")

	// Call Guest 1
	callResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/call", nil, util.AuthCookie(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, callResp.StatusCode)

	var callData map[string]any
	require.NoError(t, util.DecodeJSON(callResp, &callData))
	callResp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Try to call Guest 2 without serving Guest 1 — blocked in strict mode
	call2Resp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/call", nil, util.AuthCookie(token))
	require.NoError(t, err)
	defer call2Resp.Body.Close()
	assert.Equal(t, http.StatusConflict, call2Resp.StatusCode, "strict mode should block calling next until current is served")

	// Serve Guest 1
	entryID := callData["data"].(map[string]any)["id"].(string)
	serveResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/serve/"+entryID, nil, util.AuthCookie(token))
	require.NoError(t, err)
	serveResp.Body.Close()
	assert.Equal(t, http.StatusOK, serveResp.StatusCode)

	// Now should be able to call Guest 2
	call3Resp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/call", nil, util.AuthCookie(token))
	require.NoError(t, err)
	call3Resp.Body.Close()
	assert.Equal(t, http.StatusOK, call3Resp.StatusCode)
}

func TestQueue_ManualPositioning(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "Manual Host", "manual@test.com", "password")

	// Create queue with manual_positioning = true
	payload := map[string]any{
		"name":               "Manual Queue",
		"manual_positioning": true,
	}
	resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var body map[string]any
	require.NoError(t, util.DecodeJSON(resp, &body))
	data := body["data"].(map[string]any)
	queueID := data["id"].(string)
	joinCode := data["join_code"].(string)
	resp.Body.Close()

	// Join Guest — position should be 0 (manual positioning skips sorted set)
	guestPayload := map[string]any{
		"display_name": "Manual Guest",
		"join_code":    joinCode,
	}
	guestResp, err := util.POST(s, "/api/v1/customer/entry/join/"+queueID, guestPayload)
	require.NoError(t, err)
	defer guestResp.Body.Close()

	var guestData map[string]any
	require.NoError(t, util.DecodeJSON(guestResp, &guestData))
	assert.Equal(t, float64(0), guestData["data"].(map[string]any)["position"],
		"manual positioning should report position 0 from API")
}

func TestQueue_SkipGuest(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "Skip Host", "skip@test.com", "password")
	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Skip Queue", token)

	qutil.JoinQueue(t, s, queueID, joinCode, "Guest 1")
	qutil.JoinQueue(t, s, queueID, joinCode, "Guest 2")

	callResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/call", nil, util.HostCookie(token))
	require.NoError(t, err)
	defer callResp.Body.Close()
	require.Equal(t, http.StatusOK, callResp.StatusCode)

	var callBody map[string]any
	require.NoError(t, util.DecodeJSON(callResp, &callBody))
	callData := callBody["data"].(map[string]any)
	entryID := callData["id"].(string)

	skipResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/skip/"+entryID, nil, util.HostCookie(token))
	require.NoError(t, err)
	defer skipResp.Body.Close()
	assert.Equal(t, http.StatusOK, skipResp.StatusCode)

	ctx := context.Background()
	var entry bson.M
	err = s.DB.Collection("queue_entries").FindOne(ctx, bson.M{"_id": entryID}).Decode(&entry)
	require.NoError(t, err)
	assert.Equal(t, "SKIPPED", entry["status"])
	assert.NotNil(t, entry["finished_at"])

	nextResp, err := util.POST(s, "/api/v1/queue/manage/"+queueID+"/call", nil, util.HostCookie(token))
	require.NoError(t, err)
	defer nextResp.Body.Close()
	require.Equal(t, http.StatusOK, nextResp.StatusCode)

	var nextBody map[string]any
	require.NoError(t, util.DecodeJSON(nextResp, &nextBody))
	nextData := nextBody["data"].(map[string]any)
	assert.NotEqual(t, entryID, nextData["id"])
	assert.Equal(t, "Guest 2", nextData["name"])
}

func TestJoinQueue_CapacityExceeded(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a host (starts on Free plan with limit of 25 guests per queue)
	token, _, _ := authutil.RegisterHost(t, s, "Capacity Host", "caphost@test.com", "password")

	// 2. Create queue
	payload := map[string]any{
		"name": "Limited Capacity Queue",
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	queueID := data["id"].(string)
	joinCode := data["join_code"].(string)

	// 3. Join 25 guests (up to the free tier capacity limit)
	for i := 1; i <= 25; i++ {
		joinPayload := map[string]any{
			"display_name": fmt.Sprintf("Guest %d", i),
			"fingerprint":  fmt.Sprintf("fingerprint-%d", i),
			"join_code":    joinCode,
		}
		joinBody, _ := json.Marshal(joinPayload)
		req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err = s.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	// 4. Try to join 26th guest (should be rejected with 403 Forbidden and QUEUE_FULL error code)
	joinPayload := map[string]any{
		"display_name": "Guest 26",
		"fingerprint":  "fingerprint-26",
		"join_code":    joinCode,
	}
	joinBody, _ := json.Marshal(joinPayload)
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	var errBody map[string]any
	require.NoError(t, util.DecodeJSON(resp, &errBody))
	assert.Equal(t, "QUEUE_FULL", errBody["code"])
}
