package redis

import (
	"context"
	"time"

	redisdriver "github.com/redis/go-redis/v9"
)

// ExecRetry executes an operation with up to 3 retries.
// It uses a 5-second timeout per attempt and 50ms sleep between retries.
func ExecRetry(ctx context.Context, _ *redisdriver.Client, op func(context.Context) error) error {
	var err error
	for i := 0; i < 3; i++ {
		tCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = op(tCtx)
		cancel()

		if err == nil || err == redisdriver.Nil {
			return err
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// WithRetry executes an operation that returns a value with up to 3 retries.
// It uses a 5-second timeout per attempt and 50ms sleep between retries.
func WithRetry[T any](ctx context.Context, _ *redisdriver.Client, op func(context.Context) (T, error)) (T, error) {
	var result T
	var err error
	for i := 0; i < 3; i++ {
		tCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result, err = op(tCtx)
		cancel()

		if err == nil || err == redisdriver.Nil {
			return result, err
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return result, err
}
