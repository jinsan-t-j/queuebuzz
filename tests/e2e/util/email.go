package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// AssertEmailReceived queries the Mailpit API and asserts that at least one
// email was delivered to `toEmail` whose subject contains `subjectFragment`.
// Waits up to 3 seconds for async email delivery.
func AssertEmailReceived(t *testing.T, toEmail, subjectFragment string) {
	t.Helper()
	mailpitURL := os.Getenv("MAILPIT_API_URL")
	if mailpitURL == "" {
		t.Log("MAILPIT_API_URL not set, skipping email assertion")
		return
	}

	var found bool
	for attempt := 0; attempt < 6; attempt++ {
		time.Sleep(500 * time.Millisecond)

		resp, err := http.Get(fmt.Sprintf("%s/api/v1/search?query=to:%s", mailpitURL, toEmail))
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		defer resp.Body.Close()

		var result struct {
			Messages []struct {
				Subject string `json:"Subject"`
			} `json:"messages"`
			Total int `json:"total"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			continue
		}

		for _, msg := range result.Messages {
			if strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(subjectFragment)) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	assert.True(t, found, "expected email to %s with subject containing %q", toEmail, subjectFragment)
}

// ClearMailpit deletes all messages from the Mailpit inbox.
func ClearMailpit(t *testing.T) {
	t.Helper()
	mailpitURL := os.Getenv("MAILPIT_API_URL")
	if mailpitURL == "" {
		return
	}
	req, _ := http.NewRequest(http.MethodDelete, mailpitURL+"/api/v1/messages", nil)
	http.DefaultClient.Do(req)
}
