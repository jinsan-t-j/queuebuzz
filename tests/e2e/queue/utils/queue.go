package qutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/require"
)

// CreateQueue creates a queue and returns its ID and the host token.
func CreateQueue(t *testing.T, s *setup.TestSuite, name string) (string, string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"name": name})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	queueID := data["id"].(string)
	token := util.ExtractCookie(t, resp, "queuebuzz_host_token", true)
	return queueID, token
}

// JoinQueue joins a queue as a customer and returns the guest token.
func JoinQueue(t *testing.T, s *setup.TestSuite, queueID, displayName string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"display_name": displayName,
		"fingerprint":  fmt.Sprintf("fp-%s", displayName),
	})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	return util.ExtractCookie(t, resp, "guest_entry_token", true)
}

// ServeNext serves the next customer in the queue.
func ServeNext(t *testing.T, s *setup.TestSuite, queueID, hostToken string) {
	t.Helper()

	// 1. Call Next
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/queue/manage/%s/call", queueID), nil)
	req.AddCookie(util.AuthCookie(hostToken))
	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var m map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	data := m["data"].(map[string]any)
	entryID := data["id"].(string)

	// 2. Serve
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/queue/manage/%s/serve/%s", queueID, entryID), nil)
	req.AddCookie(util.AuthCookie(hostToken))
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// CreateAuthenticatedQueue creates a queue using a host access token and returns its ID.
func CreateAuthenticatedQueue(t *testing.T, s *setup.TestSuite, name string, accessToken string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"name": name})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(accessToken))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	return data["id"].(string)
}

func TerminateQueue(t *testing.T, s *setup.TestSuite, queueID string, accessToken string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/manage/"+queueID+"/terminate", nil)
	req.AddCookie(util.AuthCookie(accessToken))
	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
