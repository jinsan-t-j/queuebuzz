package setup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"queuebuzz/internal/app"
	"queuebuzz/internal/config"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/log"
	"sync"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/gofiber/fiber/v3"
	"github.com/ilyakaznacheev/cleanenv"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// TestSuite wraps the in-process Fiber app and database handles.
type TestSuite struct {
	App     *app.App
	DB      *mongodriver.Database
	T       *testing.T
	BaseURL string
	Proxy   *toxiproxy.Client
	Client  *http.Client
}

var (
	sharedSuite     *TestSuite
	sharedSuiteOnce sync.Once
)

// MockSender satisfies firebase.NotificationSender for testing.
type MockSender struct{}

func (*MockSender) SendToUser(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}
func (*MockSender) SendToMultiple(_ context.Context, _ []string, _, _ string, _ map[string]string) error {
	return nil
}

// NewTestSuite initializes the shared TestSuite.
func NewTestSuite(t *testing.T, mongoURI, redisURL string, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()
	sharedSuiteOnce.Do(func() {
		sharedSuite = bootApp(t, mongoURI, redisURL, toxiproxyClient)
	})
	return &TestSuite{
		App:     sharedSuite.App,
		DB:      sharedSuite.DB,
		T:       t,
		BaseURL: sharedSuite.BaseURL,
		Proxy:   sharedSuite.Proxy,
		Client:  sharedSuite.Client,
	}
}

// TeardownSharedSuite shuts down the shared TestSuite.
func TeardownSharedSuite() {
	if sharedSuite != nil {
		sharedSuite.Shutdown()
	}
}

func bootApp(t *testing.T, mongoURI, redisURL string, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()

	cfg := &config.Config{}

	// Load static test environment variables from the first existing path
	paths := []string{"test.env", "../../test.env", "../../../test.env"}
	loaded := false
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			if err := cleanenv.ReadConfig(p, cfg); err == nil {
				loaded = true
				break
			}
		}
	}

	if !loaded {
		log.Error().Msg("Failed to load test.env for E2E tests. Falling back to default config.")
	}

	// Override with dynamic values from testcontainers and explicit test env
	cfg.DBUri = mongoURI
	cfg.RedisURL = redisURL
	cfg.AppEnv = "test"
	cfg.DisableRateLimit = true // Always disable rate limits for E2E speed

	return BootAppWithConfig(t, cfg, &MockSender{}, toxiproxyClient)
}

// BootAppWithConfig initializes a new App instance with specific configuration.
func BootAppWithConfig(t *testing.T, cfg *config.Config, sender firebase.NotificationSender, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()

	a := app.New(cfg, sender)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())

	// Wait for server to start using Fiber hooks
	ready := make(chan struct{})
	a.Fiber.Hooks().OnListen(func(_ fiber.ListenData) error {
		close(ready)
		return nil
	})

	go func() {
		if err := a.Fiber.Listener(listener); err != nil {
			if !cfg.IsTesting() {
				log.Error().Err(err).Msg("Fiber server stopped unexpectedly")
			}
		}
	}()

	select {
	case <-ready:
		// Server is listening
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fiber server to start")
	}

	return &TestSuite{
		App:     a,
		DB:      a.Container.Mongo,
		T:       t,
		BaseURL: baseURL,
		Proxy:   toxiproxyClient,
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// CleanDB drops the test database and flushes Redis.
func (s *TestSuite) CleanDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.DB != nil {
		if err := s.DB.Drop(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to drop test database")
		}
	}

	if s.App != nil && s.App.Container != nil && s.App.Container.Redis != nil {
		if err := s.App.Container.Redis.FlushAll(ctx).Err(); err != nil {
			log.Error().Err(err).Msg("Failed to flush Redis during CleanDB")
		}
	}
}

// Shutdown gracefully stops the Fiber app.
func (s *TestSuite) Shutdown() {
	if s.App != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.App.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown test suite app")
		}
	}
}

// Do executes an HTTP request against the test server.
func (s *TestSuite) Do(req *http.Request) (*http.Response, error) {
	return s.Client.Do(req)
}
