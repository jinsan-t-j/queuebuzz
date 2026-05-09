package auth

import (
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuth_AnonymousHost_HappyPath(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create queue anonymously
	queueID, hostToken := qutil.CreateQueue(t, s, "My First Queue")
	assert.NotEmpty(t, queueID)
	assert.NotEmpty(t, hostToken)

	// 2. Verify management access (protected by HostOwnerMiddleware)
	// Anonymous hosts can manage their queue (call, serve, terminate) without billing
	resp, err := util.GET(s, "/api/v1/queue/manage/"+queueID+"/events", util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	// SSE endpoint returns 200 with streaming — confirms ownership
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAuth_Logout(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	_, hostToken := qutil.CreateQueue(t, s, "Logout Test")

	// Logout
	resp, err := util.POST(s, "/api/v1/auth/logout", nil, util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify me returns 401
	resp, err = util.GET(s, "/api/v1/host/me", resp.Cookies()...)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
