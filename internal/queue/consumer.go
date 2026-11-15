package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"time"
)

type MessageHandler func(context.Context, string, *StreamPayload) error
type Consumer struct {
	client                             *Client
	streamKey, groupName, consumerName string
	batchSize, maxRetries              int
	claimCursor                        string
}

func NewConsumer(client *Client, stream, group, name string, batch int) *Consumer {
	if batch <= 0 {
		batch = 10
	}
	return &Consumer{client: client, streamKey: stream, groupName: group, consumerName: name, batchSize: batch, maxRetries: 3, claimCursor: "0-0"}
}

// Start reclaims abandoned entries as well as new work. Heartbeats protect active
// handlers from idle claims, including slow external-provider calls.
func (c *Consumer) Start(ctx context.Context, handler MessageHandler) error {
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			_ = c.client.Rdb.Set(heartbeatCtx, c.streamKey+":workers:"+c.consumerName, time.Now().UTC().Format(time.RFC3339), 15*time.Second).Err()
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for ctx.Err() == nil {
		messages, next, err := c.client.Rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: c.streamKey, Group: c.groupName, Consumer: c.consumerName, MinIdle: time.Second, Start: c.claimCursor, Count: int64(c.batchSize)}).Result()
		if err != nil && !errors.Is(err, redis.Nil) && ctx.Err() == nil {
			slog.Error("reclaim pending work", "error", err)
		}
		if next != "" {
			c.claimCursor = next
		}
		for _, msg := range messages {
			if err := c.processOne(ctx, msg, handler); err != nil {
				slog.Warn("retry failed", "id", msg.ID, "error", err)
			}
		}
		streams, err := c.client.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: c.groupName, Consumer: c.consumerName, Streams: []string{c.streamKey, ">"}, Count: 1, Block: 500 * time.Millisecond}).Result()
		if err != nil {
			if !errors.Is(err, redis.Nil) && ctx.Err() == nil {
				slog.Error("read stream", "error", err)
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				if err := c.processOne(ctx, msg, handler); err != nil {
					slog.Warn("message processing failed", "id", msg.ID, "error", err)
				}
			}
		}
	}
	return nil
}
func (c *Consumer) processOne(ctx context.Context, msg redis.XMessage, handler MessageHandler) error {
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		return c.deadLetter(ctx, msg, "invalid payload format")
	}
	var payload StreamPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return c.deadLetter(ctx, msg, "invalid payload JSON")
	}
	pending, err := c.client.Rdb.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: c.streamKey, Group: c.groupName, Start: msg.ID, End: msg.ID, Count: 1}).Result()
	if err != nil {
		return err
	}
	if len(pending) > 0 && pending[0].RetryCount > int64(c.maxRetries) {
		return c.deadLetter(ctx, msg, "retry limit exceeded")
	}
	workCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-workCtx.Done():
				return
			case <-ticker.C:
				// JUSTID renews the lease without incrementing the delivery count.
				_ = c.client.Rdb.XClaimJustID(workCtx, &redis.XClaimArgs{Stream: c.streamKey, Group: c.groupName, Consumer: c.consumerName, MinIdle: 0, Messages: []string{msg.ID}}).Err()
			}
		}
	}()
	err = handler(workCtx, msg.ID, &payload)
	close(done)
	<-heartbeatDone
	if err != nil {
		return err
	}
	ackCtx, ackCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer ackCancel()
	return c.client.Rdb.XAck(ackCtx, c.streamKey, c.groupName, msg.ID).Err()
}
func (c *Consumer) deadLetter(ctx context.Context, msg redis.XMessage, reason string) error {
	raw, _ := json.Marshal(msg.Values)
	script := `local p=redis.call('XPENDING',KEYS[1],ARGV[1],ARGV[2],ARGV[2],1); if #p==0 then return 0 end; redis.call('XADD',KEYS[2],'*','original_id',ARGV[2],'group',ARGV[1],'reason',ARGV[3],'entry',ARGV[4]); return redis.call('XACK',KEYS[1],ARGV[1],ARGV[2])`
	if err := c.client.Rdb.Eval(ctx, script, []string{c.streamKey, c.streamKey + ":dead"}, c.groupName, msg.ID, reason, string(raw)).Err(); err != nil {
		return fmt.Errorf("dead-letter: %w", err)
	}
	slog.Error("message dead-lettered", "id", msg.ID, "reason", reason)
	return nil
}
