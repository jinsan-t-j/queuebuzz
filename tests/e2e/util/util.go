package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"queuebuzz/tests/e2e/setup"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	bodyBytes, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(bodyBytes, &m)
	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Logf("DEBUG: DecodedBody got nil data. Full body: %s", string(bodyBytes))
	}
	return data
}

// DecodeJSON decodes the response body into the target interface.
func DecodeJSON(resp *http.Response, target any) error {
	return json.NewDecoder(resp.Body).Decode(target)
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

// DELETE sends a DELETE request.
func DELETE(s *setup.TestSuite, path string, cookies ...*http.Cookie) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodDelete, s.BaseURL+path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}

// DELETEWithBody sends a DELETE request with a JSON body.
func DELETEWithBody(s *setup.TestSuite, path string, body any, cookies ...*http.Cookie) (*http.Response, error) {
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodDelete, s.BaseURL+path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return s.Do(req)
}
