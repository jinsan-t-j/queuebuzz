package customer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestCustomer_RecoveryEmailAndTokenFlow(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Recovery Clinic")
	entryID, guestToken := joinQueueWithEmail(t, s, queueID, joinCode, "guest@example.com")

	var stored struct {
		RecoveryEmailSentAt *time.Time `bson:"recovery_email_sent_at"`
	}
	// Sleep briefly to allow goroutine to finish
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, s.DB.Collection("queue_entries").FindOne(context.Background(), bson.M{"_id": entryID}).Decode(&stored))
	require.NotNil(t, stored.RecoveryEmailSentAt)

	util.AssertEmailReceived(t, "guest@example.com", "Recover your queue position")
	token := util.ExtractTokenFromEmailBody(t, "guest@example.com", "Recover your queue position", "token=")
	require.NotEmpty(t, token)

	resp, err := util.GET(s, "/api/v1/customer/entry/recover-by-token?token="+url.QueryEscape(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	util.ExtractCookie(t, resp, "guest_entry_token", true)

	data := util.DecodedBody(t, resp)
	require.Equal(t, entryID, data["id"].(string))

	replayResp, err := util.GET(s, "/api/v1/customer/entry/recover-by-token?token="+url.QueryEscape(token))
	require.NoError(t, err)
	defer replayResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, replayResp.StatusCode)

	// The recovered session should still match the original guest token session.
	guestResp, err := util.GET(s, "/api/v1/customer/entry", util.GuestCookie(guestToken))
	require.NoError(t, err)
	defer guestResp.Body.Close()
	assert.Equal(t, http.StatusOK, guestResp.StatusCode)
}

func TestCustomer_RecoveryEndpoint_Errors(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	missingResp, err := util.GET(s, "/api/v1/customer/entry/recover-by-token")
	require.NoError(t, err)
	defer missingResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, missingResp.StatusCode)

	invalidResp, err := util.GET(s, "/api/v1/customer/entry/recover-by-token?token=malformed.token")
	require.NoError(t, err)
	defer invalidResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, invalidResp.StatusCode)
}

func TestCustomer_RecoveryEmail_SentOnProfileUpdateOnce(t *testing.T) {
	s := Suite(t)
	s.CleanDB()
	util.ClearMailpit(t)

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "Profile Update Queue")
	entryID, guestToken := joinQueueWithoutEmail(t, s, queueID, joinCode)

	firstPayload := map[string]any{"email": "first-recovery@example.com"}
	resp, err := util.POST(s, "/api/v1/customer/entry/update", firstPayload, util.GuestCookie(guestToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stored struct {
		RecoveryEmailSentAt *time.Time `bson:"recovery_email_sent_at"`
		Email               *string    `bson:"email"`
	}
	// Sleep briefly to allow goroutine to finish
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, s.DB.Collection("queue_entries").FindOne(context.Background(), bson.M{"_id": entryID}).Decode(&stored))
	require.NotNil(t, stored.RecoveryEmailSentAt)
	require.NotNil(t, stored.Email)
	require.Equal(t, "first-recovery@example.com", *stored.Email)

	util.AssertEmailReceived(t, "first-recovery@example.com", "Recover your queue position")

	util.ClearMailpit(t)
	secondPayload := map[string]any{"email": "second-recovery@example.com"}
	resp, err = util.POST(s, "/api/v1/customer/entry/update", secondPayload, util.GuestCookie(guestToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assertNoEmailReceived(t, "second-recovery@example.com")
}

func joinQueueWithEmail(t *testing.T, s *setup.TestSuite, queueID, joinCode, email string) (string, string) {
	t.Helper()
	return joinQueueWithOptionalEmail(t, s, queueID, joinCode, email)
}

func joinQueueWithoutEmail(t *testing.T, s *setup.TestSuite, queueID, joinCode string) (string, string) {
	t.Helper()
	return joinQueueWithOptionalEmail(t, s, queueID, joinCode, "")
}

func joinQueueWithOptionalEmail(t *testing.T, s *setup.TestSuite, queueID, joinCode, email string) (string, string) {
	t.Helper()
	payload := map[string]any{
		"display_name": "Recovery Guest",
		"fingerprint":  fmt.Sprintf("fp-%s", queueID),
		"join_code":    joinCode,
	}
	if email != "" {
		payload["email"] = email
	}

	resp, err := util.POST(s, fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), payload)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	entryID := data["id"].(string)
	guestToken := util.ExtractCookie(t, resp, "guest_entry_token", true)

	return entryID, guestToken
}

func assertNoEmailReceived(t *testing.T, toEmail string) {
	t.Helper()
	mailpitURL := os.Getenv("MAILPIT_API_URL")
	if mailpitURL == "" {
		t.Log("MAILPIT_API_URL not set, skipping no-email assertion")
		return
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/search?query=to:%s", mailpitURL, toEmail))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 0, result.Total, "Should not have received an email")
}
