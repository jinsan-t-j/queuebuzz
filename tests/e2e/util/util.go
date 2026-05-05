package util

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"queuebuzz/tests/e2e/setup"
	"strings"
	"testing"
	"time"

	stdwebhook "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// ExtractCookie retrieves a cookie value by name.
func ExtractCookie(t *testing.T, resp *http.Response, name string, mustBeHTTPOnly bool) string {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			if mustBeHTTPOnly {
				require.True(t, c.HttpOnly, "cookie %q must be HttpOnly", name)
			}
			return c.Value
		}
	}
	t.Fatalf("cookie %q not found in response", name)
	return ""
}

// AuthCookie returns an http.Cookie for the authenticated host access token.
func AuthCookie(token string) *http.Cookie {
	return &http.Cookie{Name: "access_token", Value: token}
}

// HostCookie returns an http.Cookie for the host token.
func HostCookie(token string) *http.Cookie {
	return &http.Cookie{Name: "queuebuzz_host_token", Value: token}
}

// GuestCookie returns an http.Cookie for the guest token.
func GuestCookie(token string) *http.Cookie {
	return &http.Cookie{Name: "guest_entry_token", Value: token}
}

// DecodedBody extracts the 'data' field from a JSON response.
func DecodedBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	data, _ := m["data"].(map[string]any)
	return data
}

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

	data := DecodedBody(t, resp)
	queueID := data["id"].(string)
	token := ExtractCookie(t, resp, "queuebuzz_host_token", true)
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

	return ExtractCookie(t, resp, "guest_entry_token", true)
}

// ReadSSE streams events from an SSE endpoint.
func ReadSSE(ctx context.Context, s *setup.TestSuite, path string, cookies []*http.Cookie) (<-chan SSEEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a fresh client for SSE to avoid connection pooling/cookie jar interference
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("sse: status %d", resp.StatusCode)
	}

	ch := make(chan SSEEvent, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		var ev SSEEvent
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			if line == "" {
				if ev.Data != "" || ev.Event != "" {
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
					ev = SSEEvent{}
				}
				continue
			}
			if k, v, ok := strings.Cut(line, ":"); ok {
				switch k {
				case "event":
					ev.Event = strings.TrimSpace(v)
				case "data":
					ev.Data = strings.TrimSpace(v)
				case "id":
					ev.ID = strings.TrimSpace(v)
				}
			}
		}
	}()

	return ch, nil
}

// POST sends a JSON POST request.
func POST(s *setup.TestSuite, path string, body any, cookies ...*http.Cookie) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}

// POSTAuth sends a JSON POST request with a host token.
func POSTAuth(s *setup.TestSuite, path string, body any, token string) (*http.Response, error) {
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(HostCookie(token))
	return s.Do(req)
}

// GET sends a GET request.
func GET(s *setup.TestSuite, path string, cookies ...*http.Cookie) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodGet, s.BaseURL+path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}

// PATCH sends a JSON PATCH request.
func PATCH(s *setup.TestSuite, path string, body any, cookies ...*http.Cookie) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPatch, s.BaseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}

// toFormData converts fields and files into a multipart buffer and returns the content type.
func toFormData(fields map[string]string, files map[string][]byte) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range fields {
		_ = writer.WriteField(k, v)
	}

	for k, v := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s.png"`, k, k))
		h.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		_, _ = part.Write(v)
	}

	_ = writer.Close()
	return body, writer.FormDataContentType(), nil
}

// PATCHForm sends a multipart/form-data PATCH request.
func PATCHForm(s *setup.TestSuite, path string, fields map[string]string, files map[string][]byte, cookies ...*http.Cookie) (*http.Response, error) {
	body, contentType, err := toFormData(fields, files)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPatch, s.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}

// RegisterHost registers a new host and returns the access token.
func RegisterHost(t *testing.T, s *setup.TestSuite, name, email, _ string) string {
	t.Helper()

	hostID := "host-" + email
	publicID := "pub-" + email

	_, err := s.DB.Collection("hosts").InsertOne(context.Background(), map[string]any{
		"_id":        hostID,
		"public_id":  publicID,
		"name":       name,
		"email":      email,
		"tier":       "free",
		"created_at": time.Now(),
		"last_seen":  time.Now(),
	})
	require.NoError(t, err)

	accessToken, _, _, _, err := s.App.Container.Auth.Service.IssueTokenPair(context.Background(), hostID, publicID)
	require.NoError(t, err)

	return accessToken
}

// ServeNext serves the next customer in the queue.
func ServeNext(t *testing.T, s *setup.TestSuite, queueID, hostToken string) {
	t.Helper()

	// 1. Call Next
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/queue/manage/%s/call", queueID), nil)
	req.AddCookie(AuthCookie(hostToken))
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
	req.AddCookie(AuthCookie(hostToken))
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
	req.AddCookie(AuthCookie(accessToken))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := DecodedBody(t, resp)
	return data["id"].(string)
}

// UpgradeToPremium gives a host history access by seeding plans and creating a subscription.
func UpgradeToPremium(t *testing.T, s *setup.TestSuite, hostID string) {
	t.Helper()
	ctx := context.Background()

	planID := "premium-global"
	_, _ = s.DB.Collection("billing_plans").DeleteOne(ctx, bson.M{"_id": planID})
	_, err := s.DB.Collection("billing_plans").InsertOne(ctx, map[string]any{
		"_id":           planID,
		"slug":          "premium-global",
		"tier":          "pro",
		"name":          "Premium Global",
		"is_free":       false,
		"country_code":  "GLOBAL",
		"currency":      "USD",
		"monthly_price": 1900,
		"limits": map[string]any{
			"max_queues_per_month":   10,
			"max_guests_per_queue":   500,
			"history_access":         true,
			"custom_branding":        false,
			"can_export":             true,
			"queue_expiry_hours":     72,
			"can_view_guest_data":    true,
			"history_retention_days": 30,
		},
	})
	require.NoError(t, err)

	subID := "sub-" + hostID
	_, _ = s.DB.Collection("billing_subscriptions").DeleteOne(ctx, bson.M{"host_id": hostID})
	_, err = s.DB.Collection("billing_subscriptions").InsertOne(ctx, map[string]any{
		"_id":        subID,
		"host_id":    hostID,
		"plan_id":    planID,
		"status":     "active",
		"updated_at": time.Now(),
	})
	require.NoError(t, err)
}

// UpgradeToProPlan assigns the host the seeded pro-in-v1 plan.
func UpgradeToProPlan(t *testing.T, s *setup.TestSuite, hostID string) {
	t.Helper()
	ctx := context.Background()
	subID := "sub-" + hostID
	_, _ = s.DB.Collection("billing_subscriptions").DeleteOne(ctx, bson.M{"host_id": hostID})
	_, err := s.DB.Collection("billing_subscriptions").InsertOne(ctx, map[string]any{
		"_id":        subID,
		"host_id":    hostID,
		"plan_id":    "pro-in-v1",
		"status":     "active",
		"updated_at": time.Now(),
	})
	require.NoError(t, err)
	// Clear any cached plan
	s.App.Container.Redis.Del(ctx, "billing:host_plan:"+hostID)
}

// DowngradeToFree removes the subscription and clears billing cache.
func DowngradeToFree(t *testing.T, s *setup.TestSuite, hostID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = s.DB.Collection("billing_subscriptions").DeleteMany(ctx, bson.M{"host_id": hostID})
	s.App.Container.Redis.Del(ctx, "billing:host_plan:"+hostID)
}

// SignWebhook signs a payload using standard-webhooks and returns headers.
func SignWebhook(t *testing.T, key string, payload []byte) map[string]string {
	t.Helper()
	wh, err := stdwebhook.NewWebhook(key)
	require.NoError(t, err)

	webhookID := "wh_" + fmt.Sprintf("%d", time.Now().UnixNano())
	timestamp := time.Now()

	signature, err := wh.Sign(webhookID, timestamp, payload)
	require.NoError(t, err)

	return map[string]string{
		"webhook-id":        webhookID,
		"webhook-signature": signature,
		"webhook-timestamp": fmt.Sprintf("%d", timestamp.Unix()),
	}
}

// SendWebhook signs a payload with the test webhook key and POSTs it to the
// billing webhook endpoint. Returns the HTTP response for assertion.
func SendWebhook(t *testing.T, s *setup.TestSuite, payload []byte) *http.Response {
	t.Helper()
	const testWebhookKey = "whsec_dGVzdF93ZWJob29rX2tleV8xMjM0NTY3ODkwMTI="
	headers := SignWebhook(t, testWebhookKey, payload)

	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/billing/webhook", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.Do(req)
	require.NoError(t, err)
	return resp
}

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
