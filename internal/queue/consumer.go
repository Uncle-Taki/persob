package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type MessageHandler func(ctx context.Context, msgID string, payload *StreamPayload) error

type Consumer struct {
	client       *Client
	streamKey    string
	groupName    string
	consumerName string
	batchSize    int
	maxRetries   int
}

func NewConsumer(client *Client, streamKey, groupName, consumerName string, batchSize int) *Consumer {
	if batchSize <= 0 {
		batchSize = 10
	}
	return &Consumer{
		client:       client,
		streamKey:    streamKey,
		groupName:    groupName,
		consumerName: consumerName,
		batchSize:    batchSize,
		maxRetries:   3,
	}
}

// Start consumes messages continuously until ctx is cancelled
func (c *Consumer) Start(ctx context.Context, handler MessageHandler) error {
	slog.Info("starting redis stream consumer",
		"stream", c.streamKey,
		"group", c.groupName,
		"consumer", c.consumerName,
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("consumer stopping due to context cancellation")
			return nil
		default:
		}

		// Read new messages with XReadGroup
		streams, err := c.client.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.groupName,
			Consumer: c.consumerName,
			Streams:  []string{c.streamKey, ">"},
			Count:    int64(c.batchSize),
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			slog.Error("redis xreadgroup error", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(1 * time.Second):
			}
			continue
		}

		for _, stream := range streams {
			for _, xMsg := range stream.Messages {
				if err := c.processOne(ctx, xMsg, handler); err != nil {
					slog.Error("failed to process stream message", "id", xMsg.ID, "error", err)
				}
			}
		}
	}
}

func (c *Consumer) processOne(ctx context.Context, xMsg redis.XMessage, handler MessageHandler) error {
	rawPayload, ok := xMsg.Values["payload"].(string)
	if !ok {
		// Bad message structure, ack and discard
		_ = c.client.Rdb.XAck(ctx, c.streamKey, c.groupName, xMsg.ID).Err()
		return fmt.Errorf("invalid payload format for msg %s", xMsg.ID)
	}

	var payload StreamPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		_ = c.client.Rdb.XAck(ctx, c.streamKey, c.groupName, xMsg.ID).Err()
		return fmt.Errorf("unmarshal stream payload: %w", err)
	}

	// Execute handler
	err := handler(ctx, xMsg.ID, &payload)
	if err != nil {
		// Log error; can be retried or sent to DLQ
		slog.Warn("handler error for stream message", "msg_id", xMsg.ID, "error", err)
		return err
	}

	// Success, acknowledge message
	if ackErr := c.client.Rdb.XAck(ctx, c.streamKey, c.groupName, xMsg.ID).Err(); ackErr != nil {
		slog.Error("failed to xack message", "msg_id", xMsg.ID, "error", ackErr)
		return ackErr
	}

	return nil
}
