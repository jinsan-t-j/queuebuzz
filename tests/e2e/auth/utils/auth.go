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

	// 1. Request Magic Link
	payload := map[string]string{"email": email}
	resp, err := util.POST(s, "/api/v1/auth/login", payload)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Get Magic Link from Mailpit
	var token string
	for i := 0; i < 10; i++ {
		token = GetMagicLinkToken(t, s.MailpitURL, email)
		if token != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NotEmpty(t, token, "magic link token not found in Mailpit")

	// 3. Verify Token
	verifyResp, err := util.GET(s, "/auth/verify?token="+token)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, verifyResp.StatusCode)

	accessToken := util.ExtractCookie(t, verifyResp, "access_token", true)
	require.NotEmpty(t, accessToken)

	// Get Host info to get userID since we don't have it in the response body anymore
	hostData := GetUser(t, s, accessToken)
	userID, _ := hostData["id"].(string)

	// 4. Update Profile (Name)
	updateResp, err := util.PATCHWithAuth(s, "/api/v1/host/me", map[string]string{"name": name}, accessToken)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, updateResp.StatusCode)

	// 5. Get Real Public ID
	publicID, _ := hostData["public_id"].(string)

	return accessToken, userID, publicID
}

func GetUser(t *testing.T, s *setup.TestSuite, accessToken string) map[string]interface{} {
	t.Helper()
	resp, err := util.GETWithAuth(s, "/api/v1/host/me", accessToken)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	return result.Data
}

// GetMagicLinkToken scrapes Mailpit for the latest magic link token for a specific email.
func GetMagicLinkToken(t *testing.T, mailpitURL, toEmail string) string {
	t.Helper()
	if mailpitURL == "" {
		mailpitURL = os.Getenv("MAILPIT_API_URL")
	}
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
			ID string `json:"ID"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Messages) == 0 {
		return ""
	}

	// Get latest message details
	msgID := result.Messages[0].ID
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/message/%s", mailpitURL, msgID))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var msg struct {
		HTML string `json:"HTML"`
		Text string `json:"Text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return ""
	}

	// Extract token from link
	body := msg.Text + msg.HTML
	start := "token="
	idx := strings.Index(body, start)
	if idx == -1 {
		return ""
	}

	token := body[idx+len(start):]
	// Cut off at first non-alphanumeric/dot/dash character
	for i, char := range token {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '-') {
			return token[:i]
		}
	}

	return token
}
