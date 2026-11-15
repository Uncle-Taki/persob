package queue

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestPublishMessageMarshalError drives producer.go's json.Marshal failure
// branch: a NaN float in MediaMetadata is not JSON-serializable, so the marshal
// call returns an error before any stream write occurs.
func TestPublishMessageMarshalError(t *testing.T) {
	client := newTestClient(t)
	stream, _ := streamNames(t)
	p := NewProducer(client, stream)
	msg := makeMessage("unserializable")
	msg.MediaMetadata = map[string]interface{}{"bad": math.NaN()}

	id, err := p.PublishMessage(context.Background(), msg)
	if err == nil {
		t.Fatal("PublishMessage with NaN metadata: expected marshal error, got nil")
	}
	if id != "" {
		t.Fatalf("PublishMessage error path returned non-empty ID %q", id)
	}
	// Nothing must have been written to the stream.
	l, lerr := client.Rdb.XLen(context.Background(), stream).Result()
	if lerr != nil {
		t.Fatalf("XLen: %v", lerr)
	}
	if l != 0 {
		t.Fatalf("stream has %d entries after marshal failure, want 0", l)
	}
}

// TestProcessOneHeartbeatTicker drives the per-message heartbeat goroutine's
// ticker.C case (consumer.go:111): a handler that outlives the 250ms tick makes
// the XClaimJustID lease renewal run at least once while the handler is active.
func TestProcessOneHeartbeatTicker(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	pubID, err := p.PublishMessage(ctx, makeMessage("heartbeat"))
	if err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}
	msg, dcancel := runDirectProcessOne(t, client.Rdb, stream, group, "c-hb")
	defer dcancel()
	if msg.ID != pubID {
		t.Fatalf("seed ID %q != published %q", msg.ID, pubID)
	}

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-hb", 5)
	// Outlive the 250ms heartbeat tick so the ticker.C -> XClaimJustID path runs.
	if err := c.processOne(ctx, msg, func(context.Context, string, *StreamPayload) error {
		called.Store(true)
		time.Sleep(600 * time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("processOne with slow handler: %v", err)
	}
	if !called.Load() {
		t.Fatal("handler was not called")
	}
	if pc := pendingCount(t, client.Rdb, stream, group); pc != 0 {
		t.Fatalf("pending after slow handler = %d, want 0", pc)
	}
}

// TestProcessOneDeadLetterEvalError drives deadLetter's Eval failure branch
// (consumer.go:129). Pointing the consumer at a closed-socket client makes the
// dead-letter Lua script fail, so processOne must return the wrapped error
// instead of nil. An invalid payload format routes to deadLetter before any
// pending-entry lookup, so the lookup failure is not a confounder.
func TestProcessOneDeadLetterEvalError(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-dlerr", 5)
	c.client = &Client{Rdb: redis.NewClient(&redis.Options{Addr: closedPortNoT(), DialTimeout: 200 * time.Millisecond, PoolSize: 1})}
	err := c.processOne(ctx, redis.XMessage{ID: "1-1", Values: map[string]interface{}{"payload": 42}}, func(context.Context, string, *StreamPayload) error {
		called.Store(true)
		return nil
	})
	if err == nil {
		t.Fatal("processOne with failing dead-letter Eval: expected error, got nil")
	}
	if called.Load() {
		t.Fatal("handler must not run when dead-lettering fails")
	}
}

// TestProcessOneWorkCtxCancelled drives the heartbeat goroutine's workCtx.Done
// case (consumer.go:108): a parent context that is cancelled while the handler is
// running makes workCtx (a 45s child) fire, so the ticker goroutine exits via
// workCtx.Done() rather than the done signal.
func TestProcessOneWorkCtxCancelled(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	if _, err := p.PublishMessage(ctx, makeMessage("ctx-cancel")); err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}
	msg, dcancel := runDirectProcessOne(t, client.Rdb, stream, group, "c-cxl")
	defer dcancel()

	// Cancel the parent mid-handler; workCtx derives from it so the heartbeat
	// goroutine must observe workCtx.Done().
	parent, pcancel := context.WithCancel(context.Background())
	defer pcancel()

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-cxl", 5)
	done := make(chan error, 1)
	go func() {
		done <- c.processOne(parent, msg, func(context.Context, string, *StreamPayload) error {
			called.Store(true)
			time.Sleep(300 * time.Millisecond)
			return nil
		})
	}()
	// Give the handler time to start, then cancel the parent before workCtx's
	// 45s deadline, forcing the workCtx.Done() exit path in the ticker goroutine.
	time.Sleep(100 * time.Millisecond)
	pcancel()

	select {
	case err := <-done:
		// A cancelled parent is not a handler error: processOne still ACKs via a
		// context.WithoutCancel child, so a nil result is the expected success path.
		if err != nil {
			t.Fatalf("processOne after parent cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processOne did not return after parent cancel")
	}
	if !called.Load() {
		t.Fatal("handler was not called")
	}
}
