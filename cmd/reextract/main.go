// Command reextract replays retained raw Telegram messages through the normal
// worker stream. It is intentionally opt-in: without --enqueue it only scans
// and reports the workload, and it never contacts Telegram.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
)

type checkpoint struct {
	ReceivedAt      time.Time `json:"received_at"`
	TelegramChatID  int64     `json:"telegram_chat_id"`
	TelegramMessage int64     `json:"telegram_message_id"`
}

func loadCheckpoint(path string) (checkpoint, error) {
	if path == "" {
		return checkpoint{}, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return checkpoint{}, nil
	}
	if err != nil {
		return checkpoint{}, fmt.Errorf("read checkpoint: %w", err)
	}
	var cp checkpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return checkpoint{}, fmt.Errorf("decode checkpoint: %w", err)
	}
	return cp, nil
}

func saveCheckpoint(path string, cp checkpoint) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".reextract-checkpoint-*")
	if err != nil {
		return fmt.Errorf("create checkpoint: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect checkpoint: %w", err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err == nil {
		_, err = tmp.Write(append(data, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}
	return nil
}

func main() {
	var (
		batchSize      = flag.Int64("batch-size", 100, "messages per MongoDB page (1-1000)")
		maxMessages    = flag.Int64("max-messages", 0, "stop after this many messages; 0 means all retained messages")
		checkpointPath = flag.String("checkpoint", "", "private JSON keyset checkpoint to resume from")
		since          = flag.String("since", "", "start after this RFC3339 received_at timestamp")
		enqueue        = flag.Bool("enqueue", false, "publish messages to Redis for normal worker processing")
	)
	flag.Parse()
	if *batchSize < 1 || *batchSize > 1000 {
		fatal("--batch-size must be between 1 and 1000")
	}
	if *enqueue && *checkpointPath == "" {
		fatal("--enqueue requires --checkpoint so the replay can resume safely")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		fatal("load configuration: %v", err)
	}
	var cp checkpoint
	if *checkpointPath != "" {
		cp, err = loadCheckpoint(*checkpointPath)
		if err != nil {
			fatal("load checkpoint: %v", err)
		}
	}
	if *since != "" && cp.ReceivedAt.IsZero() {
		cp.ReceivedAt, err = time.Parse(time.RFC3339, *since)
		if err != nil {
			fatal("parse --since: %v", err)
		}
	}

	mongoDB, err := mongodb.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		fatal("connect MongoDB: %v", err)
	}
	defer mongoDB.Close(context.Background())
	messages := mongodb.NewMessagesRepo(mongoDB)
	var producer *queue.Producer
	var redisClient *queue.Client
	if *enqueue {
		redisClient, err = queue.Connect(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			fatal("connect Redis: %v", err)
		}
		defer redisClient.Close()
		if err := redisClient.EnsureConsumerGroup(ctx, cfg.RedisStreamKey, cfg.RedisConsumerGroup); err != nil {
			fatal("ensure worker consumer group: %v", err)
		}
		producer = queue.NewProducer(redisClient, cfg.RedisStreamKey)
	}

	var scanned, published int64
	for {
		if *maxMessages > 0 && scanned >= *maxMessages {
			break
		}
		limit := *batchSize
		if *maxMessages > 0 && remaining := *maxMessages - scanned; remaining < limit {
			limit = remaining
		}
		batch, err := messages.ListForReextract(ctx, cp.ReceivedAt, cp.TelegramChatID, cp.TelegramMessage, limit)
		if err != nil {
			fatal("list retained messages: %v", err)
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			msg := &batch[i]
			if producer != nil {
				if _, err := producer.PublishMessage(ctx, msg); err != nil {
					fatal("publish %d/%d: %v", msg.TelegramChatID, msg.TelegramMessageID, err)
				}
				published++
			}
			scanned++
			cp = checkpoint{ReceivedAt: msg.ReceivedAt, TelegramChatID: msg.TelegramChatID, TelegramMessage: msg.TelegramMessageID}
		}
		if *enqueue {
			if err := saveCheckpoint(*checkpointPath, cp); err != nil {
				fatal("save checkpoint: %v", err)
			}
		}
		if int64(len(batch)) < limit {
			break
		}
	}

	result := map[string]any{"status": "scanned", "messages_scanned": scanned, "messages_published": published, "enqueue": *enqueue}
	if *checkpointPath != "" {
		result["checkpoint"] = *checkpointPath
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		slog.Error("write result", "error", err)
		os.Exit(1)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "reextract: "+format+"\n", args...)
	os.Exit(1)
}
