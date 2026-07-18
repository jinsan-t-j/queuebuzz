package redis

import (
	"context"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/log"

	redisdriver "github.com/redis/go-redis/v9"
)

func Connect(cfg *config.Config) *redisdriver.Client {
	opts, err := redisdriver.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse Redis URL")
	}

	if cfg.RedisPassword != "" {
		opts.Password = cfg.RedisPassword
	}

	// Optimize connection pool and timeouts for high concurrency load testing
	opts.PoolSize = cfg.RedisMaxPoolSize
	opts.MinIdleConns = cfg.RedisMinIdleConns
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolTimeout = 4 * time.Second

	client := redisdriver.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping Redis")
	}

	if err := client.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		log.Warn().Err(err).Msg("Failed to enable keyspace notifications (may require admin access)")
	}

	log.Info().Msg("Connected to Redis")
	return client
}
