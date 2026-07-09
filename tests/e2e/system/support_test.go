package system

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystem_SubmitSupport(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	t.Run("Valid support request is submitted successfully and email is sent", func(t *testing.T) {
		util.ClearMailpit(t)

		payload := map[string]any{
			"name":    "John Support",
			"email":   "john.support@example.com",
			"subject": "Need help with settings",
			"message": "Hello, I am having trouble configuring my queue settings. Please assist.",
		}

		resp, err := util.POST(s, "/api/v1/support", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]any
		require.NoError(t, util.DecodeJSON(resp, &body))
		assert.Equal(t, "Support request submitted successfully", body["message"])

		// Wait briefly for background job to dispatch the email to Mailpit
		time.Sleep(500 * time.Millisecond)

		// Assert email was received in Mailpit
		mailpitURL := os.Getenv("MAILPIT_API_URL")
		if mailpitURL == "" {
			t.Log("MAILPIT_API_URL not set, skipping email verification")
			return
		}

		// Support email is configured to support@test.com in system settings seed
		searchResp, err := http.Get(fmt.Sprintf("%s/api/v1/search?query=to:support@test.com", mailpitURL))
		require.NoError(t, err)
		defer searchResp.Body.Close()

		var result struct {
			Total    int `json:"total"`
			Messages []struct {
				Subject string `json:"Subject"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(searchResp.Body).Decode(&result))
		assert.GreaterOrEqual(t, result.Total, 1, "Should have received support request email")
		if len(result.Messages) > 0 {
			assert.Contains(t, result.Messages[0].Subject, "[Support Request] Need help with settings")
		}
	})

	t.Run("Invalid support request validation errors", func(t *testing.T) {
		payloads := []map[string]any{
			{
				"name":    "", // empty
				"email":   "john.support@example.com",
				"subject": "Need help",
				"message": "Hello world",
			},
			{
				"name":    "John",
				"email":   "invalid-email", // invalid email
				"subject": "Need help",
				"message": "Hello world",
			},
			{
				"name":    "John",
				"email":   "john@example.com",
				"subject": "", // empty subject
				"message": "Hello world",
			},
			{
				"name":    "John",
				"email":   "john@example.com",
				"subject": "Need help",
				"message": "too short", // too short message (< 10 chars)
			},
		}

		for i, payload := range payloads {
			t.Run(fmt.Sprintf("Case %d", i), func(t *testing.T) {
				resp, err := util.POST(s, "/api/v1/support", payload)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			})
		}
	})
}
