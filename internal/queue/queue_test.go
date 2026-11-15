package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/redis/go-redis/v9"
)

// redisTestAddr returns the disposable redis address, honoring PERSO_QUEUE_REDIS_ADDR.
func redisTestAddr() string {
	if addr := strings.TrimSpace(os.Getenv("PERSO_QUEUE_REDIS_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:56389"
}

// closedPort returns a local TCP port that is currently closed, for failure tests.
func closedPortNoT() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// closedPort is the t-aware wrapper used by Connect failure tests.
func closedPort(t *testing.T) string {
	t.Helper()
	return closedPortNoT()
}

// newTestClient connects to the live disposable redis for the test and registers cleanup.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Connect(ctx, redisTestAddr(), "", 0)
	if err != nil {
		t.Fatalf("connect to disposable redis at %s: %v", redisTestAddr(), err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// streamNames returns unique stream/group key names for the test.
func streamNames(t *testing.T) (stream, group string) {
	t.Helper()
	suffix := uuid.NewString()
	stream = "queue_test_" + t.Name() + "_" + suffix
	group = "g_" + suffix
	return
}

// cleanupStream removes group and stream keys from redis.
func cleanupStream(t *testing.T, client *Client, stream string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Rdb.Del(ctx, stream, stream+":dead").Err()
	})
}

// makeMessage builds a deterministic TelegramMessage payload for tests.
func makeMessage(text string) *domain.TelegramMessage {
	return &domain.TelegramMessage{
		TelegramChatID:    42,
		TelegramMessageID: 7,
		ChatTitle:         "test-chat",
		ChatUsername:      "tchat",
		SenderID:          99,
		SenderUsername:    "sender",
		SenderDisplayName: "Sender Name",
		MessageText:       text,
		MessageDate:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ReceivedAt:        time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
		ProcessingStatus:  domain.ProcessingStatusPending,
	}
}

// poll waits until cond returns true, failing the test after timeout.
func poll(t *testing.T, timeout time.Duration, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, why := cond()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for condition: %s", timeout, why)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// pendingCount returns the number of pending (unACKed) entries for the group.
func pendingCount(t *testing.T, rdb *redis.Client, stream, group string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := rdb.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	return p.Count
}

// --- stream.go ---

func TestConnectSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Connect(ctx, redisTestAddr(), "", 0)
	if err != nil {
		t.Fatalf("Connect success: unexpected error: %v", err)
	}
	if client == nil || client.Rdb == nil {
		t.Fatal("Connect returned nil client or nil Rdb")
	}
	if err := client.Rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("post-Connect ping failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestConnectFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addr := closedPort(t)
	client, err := Connect(ctx, addr, "wrong-password", 0)
	if err == nil {
		_ = client.Close()
		t.Fatal("Connect to closed port: expected error, got nil")
	}
	if client != nil {
		t.Fatalf("Connect to closed port: expected nil client, got %v", client)
	}
	if !strings.Contains(err.Error(), "ping redis") {
		t.Fatalf("Connect failure error %q does not contain %q", err.Error(), "ping redis")
	}
}

func TestClientClose(t *testing.T) {
	client := newTestClient(t)
	// newTestClient registers a second Close in cleanup; a double Close must not fail.
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestEnsureConsumerGroupFreshAndExists(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fresh create must succeed on a brand-new stream.
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("fresh EnsureConsumerGroup: %v", err)
	}

	// A second create must be tolerated via the BUSYGROUP branch and return nil.
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("existing-group EnsureConsumerGroup expected nil via BUSYGROUP branch, got: %v", err)
	}

	// A third create with a different group name must also succeed (sanity).
	if err := client.EnsureConsumerGroup(ctx, stream, group+"_2"); err != nil {
		t.Fatalf("second group EnsureConsumerGroup: %v", err)
	}

	// An unknown failure path must wrap: destroy group, then point the stream at a non-stream key is not
	// possible, so use a key that already exists as a plain STRING to force a WRONGTYPE error.
	strKey := "queue_test_str_" + uuid.NewString()
	t.Cleanup(func() { _ = client.Rdb.Del(context.Background(), strKey).Err() })
	if err := client.Rdb.Set(ctx, strKey, "not-a-stream", 0).Err(); err != nil {
		t.Fatalf("seed string key: %v", err)
	}
	if err := client.EnsureConsumerGroup(ctx, strKey, "g"); err == nil {
		t.Fatal("EnsureConsumerGroup on a non-stream key: expected error, got nil")
	} else if !strings.Contains(err.Error(), "create consumer group") {
		t.Fatalf("EnsureConsumerGroup error %q does not contain %q", err.Error(), "create consumer group")
	}
}

// --- producer.go ---

func TestNewProducerAndPublishMessageRoundTrip(t *testing.T) {
	client := newTestClient(t)
	stream, _ := streamNames(t)
	cleanupStream(t, client, stream)

	p := NewProducer(client, stream)
	if p == nil {
		t.Fatal("NewProducer returned nil")
	}

	msg := makeMessage("hello queue")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := p.PublishMessage(ctx, msg)
	if err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}
	if id == "" || !strings.Contains(id, "-") {
		t.Fatalf("PublishMessage returned stream entry ID %q, expected <ms>-<seq>", id)
	}

	// Round-trip: read the stream back and unmarshal the payload.
	entries, err := client.Rdb.XRange(ctx, stream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("XRange returned %d entries, want 1", len(entries))
	}
	if entries[0].ID != id {
		t.Fatalf("stream entry ID %q != published ID %q", entries[0].ID, id)
	}
	raw, ok := entries[0].Values["payload"].(string)
	if !ok {
		t.Fatalf("entry payload field missing or not a string: %#v", entries[0].Values)
	}
	if v, _ := entries[0].Values["chat_id"]; v != fmt.Sprintf("%d", msg.TelegramChatID) {
		t.Fatalf("chat_id field = %v, want %d (redis returns fields as strings)", entries[0].Values["chat_id"], msg.TelegramChatID)
	}
	if v, _ := entries[0].Values["msg_id"]; v != fmt.Sprintf("%d", msg.TelegramMessageID) {
		t.Fatalf("msg_id field = %v, want %d (redis returns fields as strings)", entries[0].Values["msg_id"], msg.TelegramMessageID)
	}

	var got StreamPayload
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.TelegramChatID != msg.TelegramChatID || got.TelegramMessageID != msg.TelegramMessageID {
		t.Fatalf("round-trip chat/message ids = (%d,%d), want (%d,%d)",
			got.TelegramChatID, got.TelegramMessageID, msg.TelegramChatID, msg.TelegramMessageID)
	}
	if got.ChatTitle != msg.ChatTitle || got.ChatUsername != msg.ChatUsername {
		t.Fatalf("round-trip chat = (%q,%q), want (%q,%q)",
			got.ChatTitle, got.ChatUsername, msg.ChatTitle, msg.ChatUsername)
	}
	if got.SenderID != msg.SenderID || got.SenderUsername != msg.SenderUsername || got.SenderDisplayName != msg.SenderDisplayName {
		t.Fatalf("round-trip sender = (%d,%q,%q), want (%d,%q,%q)",
			got.SenderID, got.SenderUsername, got.SenderDisplayName,
			msg.SenderID, msg.SenderUsername, msg.SenderDisplayName)
	}
	if got.MessageText != msg.MessageText {
		t.Fatalf("round-trip message text = %q, want %q", got.MessageText, msg.MessageText)
	}
	if !got.MessageDate.Equal(msg.MessageDate) || !got.ReceivedAt.Equal(msg.ReceivedAt) {
		t.Fatalf("round-trip times = (%s,%s), want (%s,%s)",
			got.MessageDate, got.ReceivedAt, msg.MessageDate, msg.ReceivedAt)
	}
	if got.Message == nil || got.Message.MessageText != msg.MessageText {
		t.Fatalf("embedded Message not round-tripped: %#v", got.Message)
	}
}

func TestPublishMessageXAddError(t *testing.T) {
	// A closed port makes the real XAdd fail at dial time; assert the wrapped error.
	p := NewProducer(&Client{Rdb: redis.NewClient(&redis.Options{Addr: closedPortNoT(), DialTimeout: 200 * time.Millisecond, PoolSize: 1})}, "whatever")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.PublishMessage(ctx, makeMessage("x")); err == nil {
		t.Fatal("PublishMessage to closed redis: expected error, got nil")
	} else if !strings.Contains(err.Error(), "xadd message to stream") {
		t.Fatalf("PublishMessage error %q does not contain %q", err.Error(), "xadd message to stream")
	}
}

// --- consumer.go ---

// startConsumer starts the consumer with a cancellable context and returns the
// start goroutine's done channel (buffered, receives Start's return value) plus
// the cancel function.
func startConsumer(t *testing.T, c *Consumer, handler MessageHandler) (<-chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		err := c.Start(ctx, handler)
		done <- err
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		// With a failing XReadGroup (NOGROUP) and a cancelled context the loop skips
		// its 1s backoff and busy-retries until XAutoClaim returns, so allow up to
		// 8s for Start to unwind. `stopped` is closed by the start goroutine and can
		// be waited on here even if the test body already drained `done`.
		select {
		case <-stopped:
		case <-time.After(8 * time.Second):
			t.Error("consumer Start did not return within 8s of context cancel")
		}
	})
	return done, cancel
}

func TestConsumerStartSuccessAndAck(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}

	p := NewProducer(client, stream)
	const n = 3
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id, err := p.PublishMessage(ctx, makeMessage(fmt.Sprintf("msg-%d", i)))
		if err != nil {
			t.Fatalf("PublishMessage %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			t.Fatalf("duplicate stream entry ID %q at %d", ids[i], i)
		}
	}

	var handled atomic.Int64
	seenIDs := make(chan string, n)
	handler := func(_ context.Context, id string, pl *StreamPayload) error {
		handled.Add(1)
		seenIDs <- id
		if pl.MessageText == "" {
			return errors.New("handler: empty message text")
		}
		return nil
	}

	c := NewConsumer(client, stream, group, "c1", 0) // batch 0 must default to 10
	if c == nil {
		t.Fatal("NewConsumer returned nil")
	}
	if c.batchSize != 10 {
		t.Fatalf("NewConsumer batch default = %d, want 10", c.batchSize)
	}
	if c.maxRetries != 3 {
		t.Fatalf("NewConsumer maxRetries = %d, want 3", c.maxRetries)
	}

	startConsumer(t, c, handler)

	// All N messages must be handled and ACKed.
	poll(t, 10*time.Second, func() (bool, string) {
		if handled.Load() != n {
			return false, fmt.Sprintf("handled=%d want %d", handled.Load(), n)
		}
		if pc := pendingCount(t, client.Rdb, stream, group); pc != 0 {
			return false, fmt.Sprintf("pending=%d want 0", pc)
		}
		return true, ""
	})

	// Every published ID must have been delivered to the handler.
	got := make(map[string]bool)
	for i := 0; i < n; i++ {
		select {
		case id := <-seenIDs:
			got[id] = true
		default:
			t.Fatalf("handler did not receive message %d", i)
		}
	}
	for _, id := range ids {
		if !got[id] {
			t.Fatalf("handler never saw published ID %q (saw %v)", id, got)
		}
	}

	// Stream itself still holds the entries (XAck only clears the PEL).
	entries, err := client.Rdb.XRange(ctx, stream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange after ack: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("XRange after ack returned %d entries, want %d", len(entries), n)
	}
}

func TestConsumerRetryThenDeadLetter(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	id, err := p.PublishMessage(ctx, makeMessage("doomed"))
	if err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}

	// Handler always fails: processOne re-enters via the XAutoClaim reclaim loop
	// (MinIdle 1s) and XPendingExt.RetryCount is compared against maxRetries=3,
	// so exactly 3 handler invocations are expected before deadLetter fires.
	var handled atomic.Int64
	handler := func(_ context.Context, _ string, _ *StreamPayload) error {
		handled.Add(1)
		return errors.New("forced failure")
	}

	c := NewConsumer(client, stream, group, "retrier", 5)
	startConsumer(t, c, handler)

	// Wait until the entry is dead-lettered: the Lua script XACKs the entry
	// (clearing the PEL) and XADDs a tombstone to <stream>:dead. The original
	// entry intentionally remains in the main stream.
	poll(t, 20*time.Second, func() (bool, string) {
		if pc := pendingCount(t, client.Rdb, stream, group); pc != 0 {
			return false, fmt.Sprintf("pending=%d want 0", pc)
		}
		dl, err := client.Rdb.XLen(context.Background(), stream+":dead").Result()
		if err != nil {
			return false, err.Error()
		}
		if dl != 1 {
			return false, fmt.Sprintf("dead-letter XLen=%d want 1", dl)
		}
		return true, ""
	})

	if got := handled.Load(); got != 3 {
		t.Fatalf("handler invocations = %d, want exactly 3 (maxRetries)", got)
	}

	// Inspect the dead-letter stream.
	dead, err := client.Rdb.XRange(context.Background(), stream+":dead", "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange dead-letter: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("dead-letter stream has %d entries, want 1", len(dead))
	}
	e := dead[0]
	if v, _ := e.Values["original_id"].(string); v != id {
		t.Fatalf("dead-letter original_id = %q, want %q", v, id)
	}
	if v, _ := e.Values["group"].(string); v != group {
		t.Fatalf("dead-letter group = %q, want %q", v, group)
	}
	if v, _ := e.Values["reason"].(string); v != "retry limit exceeded" {
		t.Fatalf("dead-letter reason = %q, want %q", v, "retry limit exceeded")
	}
	raw, _ := e.Values["entry"].(string)
	var entry map[string]any
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("dead-letter entry is not valid JSON: %v (%q)", err, raw)
	}
	if _, ok := entry["payload"]; !ok {
		t.Fatalf("dead-letter entry missing original payload: %v", entry)
	}
}

// runDirectProcessOne reads a message into the pending entries list (via
// XReadGroup) and returns it, so a direct processOne call exercises the real
// XPendingExt lookup path.
func runDirectProcessOne(t *testing.T, rdb *redis.Client, stream, group, consumer string) (redis.XMessage, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: consumer,
		Streams: []string{stream, ">"}, Count: 1,
	}).Result(); err != nil {
		t.Fatalf("seed XReadGroup: %v", err)
	}
	entries, err := rdb.XRange(ctx, stream, "-", "+").Result()
	if err != nil || len(entries) != 1 {
		t.Fatalf("XRange seed: err=%v len=%d", err, len(entries))
	}
	dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
	_ = dctx
	return entries[0], dcancel
}

func TestProcessOnePendingLookupError(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	if _, err := p.PublishMessage(ctx, makeMessage("pending-err")); err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}

	msg, dcancel := runDirectProcessOne(t, client.Rdb, stream, group, "c-pend")
	defer dcancel()

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-pend", 5)
	// Swap the client to force the XPendingExt failure branch: a real client on a
	// closed socket (nil sockets panic when a command is built) with an already
	// cancelled context makes the pending lookup fail fast with a context error.
	c.client = &Client{Rdb: redis.NewClient(&redis.Options{Addr: closedPortNoT(), DialTimeout: 200 * time.Millisecond, PoolSize: 1})}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2() // fail fast: the pending lookup must return its error immediately
	err := c.processOne(ctx2, msg, func(context.Context, string, *StreamPayload) error {
		called.Store(true)
		return nil
	})
	if err == nil {
		t.Fatal("processOne with failing XPendingExt: expected error, got nil")
	}
	if called.Load() {
		t.Fatal("handler must not run when XPendingExt fails")
	}
}

func TestProcessOneInvalidPayloadFormat(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	// Entry whose payload field is a number, not a string: type-assert fails.
	rawID, err := client.Rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"payload": 12345, "chat_id": 1, "msg_id": 2},
	}).Result()
	if err != nil {
		t.Fatalf("XAdd malformed entry: %v", err)
	}

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-malformed", 5)
	// Seed the PEL so the Lua XPENDING lookup finds the entry.
	if _, err := client.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: "c-malformed",
		Streams: []string{stream, ">"}, Count: 1,
	}).Result(); err != nil {
		t.Fatalf("seed XReadGroup: %v", err)
	}
	err = c.processOne(ctx, redis.XMessage{ID: rawID, Values: map[string]any{"payload": 12345}}, func(context.Context, string, *StreamPayload) error {
		called.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("deadLetter path should return nil, got: %v", err)
	}
	if called.Load() {
		t.Fatal("handler must not run on invalid payload format")
	}

	dead, err := client.Rdb.XRange(ctx, stream+":dead", "-", "+").Result()
	if err != nil || len(dead) != 1 {
		t.Fatalf("expected 1 dead-letter entry, got %d (err=%v)", len(dead), err)
	}
	if v, _ := dead[0].Values["reason"].(string); v != "invalid payload format" {
		t.Fatalf("dead-letter reason = %q, want %q", v, "invalid payload format")
	}
}

func TestProcessOneInvalidPayloadJSON(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	rawID, err := client.Rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"payload": "{not-json", "chat_id": 1, "msg_id": 2},
	}).Result()
	if err != nil {
		t.Fatalf("XAdd malformed entry: %v", err)
	}

	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-badjson", 5)
	if _, err := client.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: "c-badjson",
		Streams: []string{stream, ">"}, Count: 1,
	}).Result(); err != nil {
		t.Fatalf("seed XReadGroup: %v", err)
	}
	err = c.processOne(ctx, redis.XMessage{ID: rawID, Values: map[string]any{"payload": "{not-json"}}, func(context.Context, string, *StreamPayload) error {
		called.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("deadLetter path should return nil, got: %v", err)
	}
	if called.Load() {
		t.Fatal("handler must not run on invalid payload JSON")
	}
	dead, err := client.Rdb.XRange(ctx, stream+":dead", "-", "+").Result()
	if err != nil || len(dead) != 1 {
		t.Fatalf("expected 1 dead-letter entry, got %d (err=%v)", len(dead), err)
	}
	if v, _ := dead[0].Values["reason"].(string); v != "invalid payload JSON" {
		t.Fatalf("dead-letter reason = %q, want %q", v, "invalid payload JSON")
	}
}

func TestProcessOneHandlerSuccessACK(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	pubID, err := p.PublishMessage(ctx, makeMessage("direct-ack"))
	if err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}

	msg, dcancel := runDirectProcessOne(t, client.Rdb, stream, group, "c-ack")
	defer dcancel()
	if msg.ID != pubID {
		t.Fatalf("seed pending ID %q != published %q", msg.ID, pubID)
	}

	var gotPayload *StreamPayload
	var called atomic.Bool
	c := NewConsumer(client, stream, group, "c-ack", 5)
	if err := c.processOne(ctx, msg, func(_ context.Context, id string, pl *StreamPayload) error {
		called.Store(true)
		gotPayload = pl
		if id != pubID {
			t.Errorf("handler ID %q != %q", id, pubID)
		}
		return nil
	}); err != nil {
		t.Fatalf("processOne success: %v", err)
	}
	if !called.Load() {
		t.Fatal("handler was not called")
	}
	if gotPayload == nil || gotPayload.MessageText != "direct-ack" {
		t.Fatalf("handler payload = %#v, want MessageText %q", gotPayload, "direct-ack")
	}
	if pc := pendingCount(t, client.Rdb, stream, group); pc != 0 {
		t.Fatalf("pending after successful processOne = %d, want 0", pc)
	}
}

func TestProcessOneHandlerErrorIsPropagated(t *testing.T) {
	client := newTestClient(t)
	stream, group := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.EnsureConsumerGroup(ctx, stream, group); err != nil {
		t.Fatalf("EnsureConsumerGroup: %v", err)
	}
	p := NewProducer(client, stream)
	if _, err := p.PublishMessage(ctx, makeMessage("fail-once")); err != nil {
		t.Fatalf("PublishMessage: %v", err)
	}

	msg, dcancel := runDirectProcessOne(t, client.Rdb, stream, group, "c-fail")
	defer dcancel()
	c := NewConsumer(client, stream, group, "c-fail", 5)
	wantErr := errors.New("boom handler")
	err := c.processOne(ctx, msg, func(context.Context, string, *StreamPayload) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("processOne error = %v, want wrapped %v", err, wantErr)
	}
	// Message must still be pending (not ACKed, not dead-lettered yet).
	if pc := pendingCount(t, client.Rdb, stream, group); pc != 1 {
		t.Fatalf("pending after failed processOne = %d, want 1", pc)
	}
	if l, _ := client.Rdb.XLen(ctx, stream+":dead").Result(); l != 0 {
		t.Fatalf("dead-letter stream has %d entries, want 0", l)
	}
}

// startConsumerErrorPath drives the XReadGroup non-nil-error branch of Start:
// pointing the consumer at a group that does not exist makes every XReadGroup
// call fail with NOGROUP, so the loop logs "read stream", backs off 1s, and
// keeps retrying until the context is cancelled.
func TestConsumerStartReadGroupErrorBranch(t *testing.T) {
	client := newTestClient(t)
	stream, _ := streamNames(t)
	cleanupStream(t, client, stream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Ensure the stream exists, but use a bogus group: XReadGroup -> NOGROUP.
	if _, err := client.Rdb.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"dummy": 1}}).Result(); err != nil {
		t.Fatalf("seed stream: %v", err)
	}
	bogusGroup := "no_such_group_" + uuid.NewString()

	// Capture the default slog output to prove the error branch logged "read stream".
	// The buffer needs a mutex: the consumer goroutine writes via slog while the test
	// goroutine polls it.
	var logMu sync.Mutex
	var logBuf bytes.Buffer
	safeLog := &lockedWriter{mu: &logMu, w: &logBuf}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(safeLog, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	var handled atomic.Int64
	c := NewConsumer(client, stream, bogusGroup, "c-err", 5)
	done, cancelStart := startConsumer(t, c, func(context.Context, string, *StreamPayload) error {
		handled.Add(1)
		return nil
	})

	// The first loop iteration reaches XReadGroup almost immediately (XAutoClaim
	// returns redis.Nil on an empty stream, no backoff). Wait deterministically
	// until the error branch has logged twice (first call + the 1s backoff).
	poll(t, 8*time.Second, func() (bool, string) {
		n := strings.Count(logString(&logMu, &logBuf), "read stream")
		return n >= 2, fmt.Sprintf("'read stream' log count=%d want >=2", n)
	})
	cancelStart()
	<-done

	// No messages were ever published to the bogus group, so the handler must not have run.
	if handled.Load() != 0 {
		t.Fatalf("handler ran %d times, want 0", handled.Load())
	}
	logOut := logString(&logMu, &logBuf)
	if n := strings.Count(logOut, "NOGROUP"); n == 0 {
		t.Fatalf("expected the captured log to contain the NOGROUP error, got: %s", logOut)
	}
}

// lockedWriter serializes concurrent writes to a log buffer.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func logString(mu *sync.Mutex, b *bytes.Buffer) string {
	mu.Lock()
	defer mu.Unlock()
	return b.String()
}
