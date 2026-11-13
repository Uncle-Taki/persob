package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	Rdb *redis.Client
}

func Connect(ctx context.Context, addr, password string, db int) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	slog.Info("connected to redis successfully", "addr", addr)
	return &Client{Rdb: rdb}, nil
}

func (c *Client) Close() error {
	return c.Rdb.Close()
}

// EnsureConsumerGroup ensures the consumer group exists on the stream
func (c *Client) EnsureConsumerGroup(ctx context.Context, stream, group string) error {
	err := c.Rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err()
	if err != nil {
		// Ignore if BUSYGROUP (group already exists)
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			return nil
		}
		return fmt.Errorf("create consumer group: %w", err)
	}
	slog.Info("created redis consumer group", "stream", stream, "group", group)
	return nil
}
