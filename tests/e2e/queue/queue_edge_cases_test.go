package queue

import (
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue_StrictMode(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "Strict Host", "strict@test.com", "password")

	// Create queue first
	queueID := qutil.CreateAuthenticatedQueue(t, s, "Strict Queue", token)

	// Enable strict mode via PATCH
	patchPayload := map[string]any{"strict_queue_mode": true}
	patchResp, err := util.PATCH(s, "/api/v1/queue/manage/"+queueID+"/", patchPayload, util.AuthCookie(token))
	require.NoError(t, err)
	patchResp.Body.Close()
	assert.Equal(t, http.StatusOK, patchResp.StatusCode)

	// Add 2 guests
	qutil.JoinQueue(t, s, queueID, "Guest 1")
	qutil.JoinQueue(t, s, queueID, "Guest 2")

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
	queueID := body["data"].(map[string]any)["id"].(string)
	resp.Body.Close()

	// Join Guest — position should be 0 (manual positioning skips sorted set)
	guestResp, err := util.POST(s, "/api/v1/customer/entry/join/"+queueID, map[string]any{"display_name": "Manual Guest"})
	require.NoError(t, err)
	defer guestResp.Body.Close()

	var guestData map[string]any
	require.NoError(t, util.DecodeJSON(guestResp, &guestData))
	assert.Equal(t, float64(0), guestData["data"].(map[string]any)["position"],
		"manual positioning should report position 0 from API")
}
