package telegram_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/telegram"
)

func TestEncryptedSessionStorage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "perso_session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	secretKey := "test-secret-key-32-bytes-long!"
	storage, err := telegram.NewEncryptedSessionStorage(tempDir, "test.session", secretKey)
	if err != nil {
		t.Fatalf("NewEncryptedSessionStorage failed: %v", err)
	}

	ctx := context.Background()

	// Initial load should be empty
	initialData, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession on non-existent file failed: %v", err)
	}
	if initialData != nil {
		t.Errorf("Expected nil session data, got %v", initialData)
	}

	// Save session
	testData := []byte("secret_mtproto_auth_key_payload_12345")
	if err := storage.StoreSession(ctx, testData); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// Load and decrypt
	loaded, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if string(loaded) != string(testData) {
		t.Errorf("Loaded data = %s, want %s", string(loaded), string(testData))
	}

	// Verify session locking
	if err := storage.LockSession(); err != nil {
		t.Fatalf("LockSession failed: %v", err)
	}

	// Attempting second lock from another instance should fail
	storage2, _ := telegram.NewEncryptedSessionStorage(tempDir, "test.session", secretKey)
	if err := storage2.LockSession(); err == nil {
		t.Errorf("Expected lock conflict error for concurrent collector, got nil")
	}

	// Unlock
	_ = storage.UnlockSession()
}

func TestMockTelegramClient(t *testing.T) {
	mock := telegram.NewMockClient()
	ctx := context.Background()

	_ = mock.Start(ctx)
	if !mock.IsConnected() {
		t.Errorf("expected client to be connected")
	}

	var receivedMsg *domain.TelegramMessage
	mock.SetMessageHandler(func(ctx context.Context, msg *domain.TelegramMessage) error {
		receivedMsg = msg
		return nil
	})

	testMsg := &domain.TelegramMessage{
		TelegramChatID:    -1001234,
		TelegramMessageID: 999,
		MessageText:       "تست پیام",
		ReceivedAt:        time.Now(),
	}

	if err := mock.InjectMessage(ctx, testMsg); err != nil {
		t.Fatalf("InjectMessage failed: %v", err)
	}

	if receivedMsg == nil || receivedMsg.TelegramMessageID != 999 {
		t.Errorf("Expected message to be received by handler, got %v", receivedMsg)
	}

	// Test sending message
	_ = mock.SendMessage(ctx, 12345, "سلام")
	sent := mock.GetSentMessages()
	if len(sent) != 1 || sent[0].Text != "سلام" {
		t.Errorf("Expected sent message record, got %v", sent)
	}
}
