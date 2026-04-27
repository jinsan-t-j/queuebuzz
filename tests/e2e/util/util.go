package util

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/setup"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
// RegisterHost registers a new host and returns the access token.
func RegisterHost(t *testing.T, s *setup.TestSuite, name, email, password string) string {
	t.Helper()
	
	hostID := "host-" + email
	publicID := "pub-" + email

	_, err := s.DB.Collection("hosts").InsertOne(context.Background(), map[string]any{
		"_id": hostID,
		"public_id": publicID,
		"name": name,
		"email": email,
		"tier": "free",
		"created_at": time.Now(),
		"last_seen": time.Now(),
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
