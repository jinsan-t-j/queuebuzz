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
	"queuebuzz/tests/e2e/billing/providers"
	"queuebuzz/tests/e2e/database/seeders"
	"sync"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/dodopayments/dodopayments-go/option"
	"github.com/gofiber/fiber/v3"
	"github.com/ilyakaznacheev/cleanenv"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// TestSuite wraps the in-process Fiber app and database handles.
type TestSuite struct {
	App        *app.App
	DB         *mongodriver.Database
	T          *testing.T
	BaseURL    string
	MailpitURL string
	Proxy      *toxiproxy.Client
	Client     *http.Client
	DodoMock   *providers.DodoMockServer
}

var (
	sharedSuite     *TestSuite
	sharedSuiteOnce sync.Once
)

type MockSender struct{}

func (*MockSender) SendToUser(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}
func (*MockSender) SendToMultiple(_ context.Context, _ []string, _, _ string, _ map[string]string) error {
	return nil
}

func NewTestSuite(t *testing.T, mongoURI, redisURL, mailpitURL string, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()
	sharedSuiteOnce.Do(func() {
		sharedSuite = bootApp(t, mongoURI, redisURL, mailpitURL, toxiproxyClient)
	})

	// Ensure MAILPIT_API_URL is set for the current process
	if mailpitURL != "" {
		os.Setenv("MAILPIT_API_URL", mailpitURL)
	}

	return &TestSuite{
		App:        sharedSuite.App,
		DB:         sharedSuite.DB,
		T:          t,
		BaseURL:    sharedSuite.BaseURL,
		MailpitURL: sharedSuite.MailpitURL,
		Proxy:      sharedSuite.Proxy,
		Client:     sharedSuite.Client,
		DodoMock:   sharedSuite.DodoMock,
	}
}

func TeardownSharedSuite() {
	if sharedSuite != nil {
		sharedSuite.Shutdown()
	}
}

func bootApp(t *testing.T, mongoURI, redisURL, mailpitURL string, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()

	cfg := &config.Config{}
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

	// Always read env to override config with container-provided URIs
	_ = cleanenv.ReadEnv(cfg)

	cfg.DBUri = mongoURI
	cfg.RedisURL = redisURL
	cfg.AppEnv = "test"
	cfg.DisableRateLimit = true

	ts := BootAppWithConfig(t, cfg, &MockSender{}, toxiproxyClient)
	ts.MailpitURL = mailpitURL
	return ts
}

func BootAppWithConfig(t *testing.T, cfg *config.Config, sender firebase.NotificationSender, toxiproxyClient *toxiproxy.Client) *TestSuite {
	t.Helper()

	dodoMock := providers.NewDodoMockServer()
	dodoOpts := []option.RequestOption{option.WithBaseURL(dodoMock.Server.URL)}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())

	cfg.AppURL = baseURL
	cfg.FrontendURL = baseURL

	a := app.New(cfg, sender, dodoOpts...)
	a.Container.Start()

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
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fiber server to start")
	}

	return &TestSuite{
		App:     a,
		DB:      a.Container.Mongo,
		T:       t,
		BaseURL: baseURL,
		Proxy:   toxiproxyClient,
		Client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		DodoMock: dodoMock,
	}
}

func (s *TestSuite) CleanDB() {
	// Give background jobs a moment to finish before dropping collections
	time.Sleep(100 * time.Millisecond)

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

	if s.DodoMock != nil {
		s.DodoMock.Reset()
	}

	s.SeedDefaults()
}

func (s *TestSuite) SeedDefaults() {
	seeders.SeedDefaults(s.DB)
}

func (s *TestSuite) Shutdown() {
	if s.App != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.App.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown test suite app")
		}
	}
	if s.DodoMock != nil && s.DodoMock.Server != nil {
		s.DodoMock.Server.Close()
	}
}

func (s *TestSuite) Do(req *http.Request) (*http.Response, error) {
	return s.Client.Do(req)
}
