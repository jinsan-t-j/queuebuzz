package system

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/middlewares"
	sentrywrap "queuebuzz/internal/sentry"

	gosentry "github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"
	fiberRecover "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport captures Sentry events in-memory instead of sending them over HTTP.
// This is the idiomatic way to test sentry-go integrations.
type mockTransport struct {
	mu     sync.Mutex
	events []*gosentry.Event
}

func (t *mockTransport) Configure(_ gosentry.ClientOptions) {}
func (t *mockTransport) SendEvent(event *gosentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}
func (t *mockTransport) Flush(_ time.Duration) bool              { return true }
func (t *mockTransport) FlushWithContext(_ context.Context) bool { return true }
func (t *mockTransport) Close()                                  {}

func (t *mockTransport) Events() []*gosentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*gosentry.Event{}, t.events...)
}

// newSentryTestApp creates a minimal Fiber app wired with our error handler
// and recover middleware — the same stack used in production.
func newSentryTestApp(t *testing.T, transport *mockTransport) *fiber.App {
	t.Helper()

	err := gosentry.Init(gosentry.ClientOptions{
		Dsn:       "http://key@localhost/1",
		Transport: transport,
	})
	require.NoError(t, err)

	cfg := &config.Config{AppEnv: "test"}
	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler: errHandler.Handle,
	})

	app.Use(fiberRecover.New(fiberRecover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(_ fiber.Ctx, e any) {
			sentrywrap.RecoverWithSentry(e)
		},
	}))

	return app
}

// containsException checks if any captured event contains the given exception value.
func containsException(events []*gosentry.Event, value string) bool {
	for _, e := range events {
		for _, ex := range e.Exception {
			if ex.Value == value {
				return true
			}
		}
	}
	return false
}

func TestSentry_CapturePanic(t *testing.T) {
	transport := &mockTransport{}
	app := newSentryTestApp(t, transport)

	app.Get("/panic", func(_ fiber.Ctx) error {
		panic("intentional-panic-value")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	events := transport.Events()
	require.NotEmpty(t, events, "Sentry should capture the panic")
	assert.True(t, containsException(events, "intentional-panic-value"))
}

func TestSentry_Capture500Error(t *testing.T) {
	transport := &mockTransport{}
	app := newSentryTestApp(t, transport)

	app.Get("/error", func(_ fiber.Ctx) error {
		return fiber.NewError(http.StatusInternalServerError, "intentional-500-error")
	})

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	events := transport.Events()
	require.NotEmpty(t, events, "Sentry should capture 5xx errors")
	assert.True(t, containsException(events, "intentional-500-error"))
}

func TestSentry_Skip4xxErrors(t *testing.T) {
	transport := &mockTransport{}
	app := newSentryTestApp(t, transport)

	app.Get("/notfound", func(_ fiber.Ctx) error {
		return fiber.NewError(http.StatusNotFound, "not-found")
	})

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	events := transport.Events()
	assert.Empty(t, events, "Sentry should NOT capture 4xx errors")
}

func TestSentry_DisabledWhenDSNEmpty(_ *testing.T) {
	// Init with empty DSN should be a no-op, not a crash.
	sentrywrap.Init("", "test", 0.1)

	// These should not panic when Sentry hub has no client.
	sentrywrap.CaptureException(errors.New("should-not-crash"))
	sentrywrap.Flush()
}
