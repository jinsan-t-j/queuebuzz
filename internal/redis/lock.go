package redis

import (
	"context"
	"time"

	"github.com/google/uuid"
	redisdriver "github.com/redis/go-redis/v9"
)

// releaseScript is compiled once and cached by go-redis by SHA hash.
var releaseScript = redisdriver.NewScript(`
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("del", KEYS[1])
	else
		return 0
	end
`)

// Lock represents a Redis distributed lock instance with ownership tracking.
type Lock struct {
	client *redisdriver.Client
	key    string
	value  string
}

// NewLock instantiates a new Lock with a cryptographically unique owner value.
// Each lock acquisition has a distinct identity, ensuring only the holder can release it.
func NewLock(client *redisdriver.Client, key string) *Lock {
	return &Lock{client: client, key: key, value: uuid.New().String()}
}

// Acquire attempts to set the lock key using SET NX PX.
func (l *Lock) Acquire(ctx context.Context, expiration time.Duration) (bool, error) {
	return l.client.SetNX(ctx, l.key, l.value, expiration).Result()
}

// Release releases the lock using an atomic Lua script, but only if the caller
// still holds it. Uses a detached context to avoid releasing with a cancelled parent.
func (l *Lock) Release(ctx context.Context) error {
	// Use a short independent context so the release is not blocked by a
	// cancelled parent (e.g. deferred cancel from WithTimeout).
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = ctx // original context intentionally unused for release
	return releaseScript.Run(releaseCtx, l.client, []string{l.key}, l.value).Err()
}
