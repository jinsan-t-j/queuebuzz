package redis

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/log"

	redisdriver "github.com/redis/go-redis/v9"
)

var (
	client *redisdriver.Client
	once   sync.Once
)

func Connect(url, password string) *redisdriver.Client {
	once.Do(func() {
		opts, err := redisdriver.ParseURL(url)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to parse Redis URL")
		}

		if password != "" {
			opts.Password = password
		}

		client = redisdriver.NewClient(opts)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Ping(ctx).Err(); err != nil {
			log.Fatal().Err(err).Msg("Failed to ping Redis")
		}

		if err := client.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
			log.Warn().Err(err).Msg("Failed to enable keyspace notifications (may require admin access)")
		}

		log.Info().Msg("Connected to Redis")
	})

	return client
}

func Disconnect() {
	if client != nil {
		if err := client.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from Redis")
		}
	}
}
