package authutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RegisterHost follows the full auth lifecycle: Login -> Magic Link (Mailpit) -> Verify -> Update Profile
func RegisterHost(t *testing.T, s *setup.TestSuite, name, email, _ string) (string, string, string) {
	t.Helper()

	// 1. Initiate Login (Sends Magic Link)
	payload := map[string]any{"email": email}
	resp, err := util.POST(s, "/api/v1/auth/login", payload)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Get Magic Link from Mailpit
	var token string
	for i := 0; i < 10; i++ {
		token = GetMagicLinkToken(t, email)
		if token != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotEmpty(t, token, "magic link token not found in Mailpit")

	// 3. Verify Magic Link (Completes Registration/Login)
	verifyURL := fmt.Sprintf("/auth/verify?token=%s", token)
	resp, err = util.GET(s, verifyURL)
	require.NoError(t, err)
	resp.Body.Close()
	require.True(t, resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther, "expected redirect status (302 or 303), got %d", resp.StatusCode)

	// 4. Extract access token from cookie
	accessToken := util.ExtractCookie(t, resp, "access_token", true)

	// 5. Update name via API
	namePayload := map[string]any{"name": name}
	nameResp, err := util.PATCH(s, "/api/v1/host/me", namePayload, util.AuthCookie(accessToken))
	require.NoError(t, err)
	nameResp.Body.Close()
	require.Equal(t, http.StatusOK, nameResp.StatusCode)

	// 6. Get Host ID via API
	resp, err = util.GET(s, "/api/v1/host/me", util.AuthCookie(accessToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var meResp struct {
		Data struct {
			ID       string `json:"id"`
			PublicID string `json:"public_id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&meResp))

	return accessToken, meResp.Data.ID, meResp.Data.PublicID
}

// GetMagicLinkToken scrapes Mailpit for the latest magic link token for a specific email.
func GetMagicLinkToken(t *testing.T, toEmail string) string {
	t.Helper()
	mailpitURL := os.Getenv("MAILPIT_API_URL")
	if mailpitURL == "" {
		return ""
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/search?query=to:%s", mailpitURL, toEmail))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Messages) == 0 {
		return ""
	}

	// Fetch the latest message body
	msgID := result.Messages[0].ID
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/message/%s", mailpitURL, msgID))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var msg struct {
		Text string `json:"Text"`
		HTML string `json:"HTML"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return ""
	}

	// Extract token from URL in body (e.g., /auth/verify?token=...)
	body := msg.Text + msg.HTML
	start := "token="
	idx := strings.Index(body, start)
	if idx == -1 {
		return ""
	}
	token := body[idx+len(start):]
	// Cut at first non-alphanumeric character (or whitespace)
	for i, char := range token {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return token[:i]
		}
	}
	return token
}
