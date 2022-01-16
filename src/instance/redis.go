package instance

import (
	"context"
	"time"
)

type Redis interface {
	Ping(ctx context.Context) error
	Subscribe(ctx context.Context, ch chan string, subscribeTo ...string)
	Publish(ctx context.Context, channel string, content string) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}
