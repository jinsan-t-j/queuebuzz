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

// GetLatestEmailBody returns the combined text/html body for the latest email
// matching the recipient and optional subject fragment.
func GetLatestEmailBody(t *testing.T, toEmail, subjectFragment string) string {
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	for _, msg := range result.Messages {
		if subjectFragment != "" && !strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(subjectFragment)) {
			continue
		}

		bodyResp, err := http.Get(fmt.Sprintf("%s/api/v1/message/%s", mailpitURL, msg.ID))
		if err != nil {
			continue
		}
		defer bodyResp.Body.Close()

		var body struct {
			Text string `json:"Text"`
			HTML string `json:"HTML"`
		}
		if err := json.NewDecoder(bodyResp.Body).Decode(&body); err != nil {
			continue
		}

		return body.Text + "\n" + body.HTML
	}

	return ""
}

// ExtractTokenFromEmailBody returns the first token value found after the given marker.
func ExtractTokenFromEmailBody(t *testing.T, toEmail, subjectFragment, marker string) string {
	t.Helper()
	body := GetLatestEmailBody(t, toEmail, subjectFragment)
	if body == "" {
		return ""
	}

	idx := strings.Index(body, marker)
	if idx == -1 {
		return ""
	}

	token := body[idx+len(marker):]
	for i, r := range token {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return token[:i]
		}
	}

	return token
}
