package security

import (
	"fmt"
	"io"
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/util"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurity_RateLimit_Triggered(t *testing.T) {
	s := Suite(t)

	cfg := *s.App.Container.Config
	cfg.DisableRateLimit = false
	testApp := setup.BootAppWithConfig(t, &cfg, &setup.MockSender{}, s.Proxy)
	defer testApp.Shutdown()

	got429 := false
	for i := 0; i < 25; i++ {
		payload := `{"email":"rate-test@example.com"}`
		resp, err := http.Post(testApp.BaseURL+"/api/v1/auth/login", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	assert.True(t, got429, "rate limiter must return 429")
}

func TestSecurity_Headers_Present(t *testing.T) {
	s := Suite(t)

	resp, err := util.GET(s, "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEmpty(t, resp.Header.Get("X-Content-Type-Options"))
	assert.NotEmpty(t, resp.Header.Get("X-Frame-Options"))
}

func TestSecurity_CORS_Origins(t *testing.T) {
	s := Suite(t)

	cases := []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"allowed", "http://localhost:3000", true},
		{"disallowed", "https://evil.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodOptions, s.BaseURL+"/api/v1/queue/p/create", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Access-Control-Request-Method", "POST")
			resp, err := s.Do(req)
			require.NoError(t, err)
			resp.Body.Close()

			if tc.allowed {
				assert.Equal(t, tc.origin, resp.Header.Get("Access-Control-Allow-Origin"))
			} else {
				assert.NotEqual(t, tc.origin, resp.Header.Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestSecurity_CrossOwner_Rejected(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	_, _, hostToken1 := qutil.CreateQueue(t, s, "Queue A")
	queueBID, _, _ := qutil.CreateQueue(t, s, "Queue B")

	resp, err := util.POST(s, fmt.Sprintf("/api/v1/queue/manage/%s/terminate", queueBID), nil, util.HostCookie(hostToken1))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSecurity_ConcurrentBurst(t *testing.T) {
	s := Suite(t)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := util.GET(s, "/healthz")
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}

func TestSecurity_Join_NoPIILeak(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "PII Test")

	payload := map[string]any{
		"display_name": "Test User",
		"email":        "user@secret.com",
		"phone":        "9999999999",
		"fingerprint":  "fp-pii",
		"join_code":    joinCode,
	}
	resp, err := util.POST(s, fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), payload)
	require.NoError(t, err)
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(raw), "9999999999")
	assert.NotContains(t, string(raw), "user@secret.com")
}
