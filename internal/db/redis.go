package db

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/log"

	"github.com/redis/go-redis/v9"
)

var (
	rdb     *redis.Client
	redOnce sync.Once
)

func ConnectRedis(url, password string) *redis.Client {
	redOnce.Do(func() {
		opts, err := redis.ParseURL(url)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to parse Redis URL")
		}

		if password != "" {
			opts.Password = password
		}

		rdb = redis.NewClient(opts)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Fatal().Err(err).Msg("Failed to ping Redis")
		}

		// Enable keyspace notifications for expired key events
		if err := rdb.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
			log.Warn().Err(err).Msg("Failed to enable keyspace notifications (may require admin access)")
		}

		log.Info().Msg("Connected to Redis")
	})

	return rdb
}

func DisconnectRedis() {
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from Redis")
		}
	}
}

func GetRedis() *redis.Client {
	return rdb
}
