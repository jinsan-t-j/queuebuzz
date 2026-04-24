package e2e

import (
	"context"
	"os"
	"queuebuzz/tests/e2e/setup"
	"testing"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	mongoURI   string
	redisURI   string
	toxiClient *toxiproxy.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start MongoDB
	mongoC, err := mongodb.Run(ctx, "mongo:6.0")
	if err != nil {
		panic(err)
	}
	mongoURI, _ = mongoC.ConnectionString(ctx)

	// Start Redis
	redisC, err := redis.Run(ctx, "redis:7-alpine")
	if err != nil {
		panic(err)
	}
	redisURI, _ = redisC.ConnectionString(ctx)

	// Start Toxiproxy (optional, but keep it if used)
	// For now, we'll pass nil if not strictly needed or if we haven't set up the container
	toxiClient = nil

	code := m.Run()

	setup.TeardownSharedSuite()
	_ = mongoC.Terminate(ctx)
	_ = redisC.Terminate(ctx)

	os.Exit(code)
}

func Suite(t *testing.T) *setup.TestSuite {
	return setup.NewTestSuite(t, mongoURI, redisURI, toxiClient)
}
