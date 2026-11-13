package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/perso/perso-mind/internal/domain"
)

type MockClient struct {
	handler   MessageHandler
	connected bool
	sentMsgs  []SentRecord
	mu        sync.RWMutex
}

type SentRecord struct {
	PeerID int64
	Text   string
	SentAt time.Time
}

func NewMockClient() *MockClient {
	return &MockClient{
		connected: false,
		sentMsgs:  make([]SentRecord, 0),
	}
}

func (m *MockClient) Start(ctx context.Context) error {
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	slog.Info("mock telegram client started")
	return nil
}

func (m *MockClient) Stop() error {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
	slog.Info("mock telegram client stopped")
	return nil
}

func (m *MockClient) SetMessageHandler(handler MessageHandler) {
	m.mu.Lock()
	m.handler = handler
	m.mu.Unlock()
}

func (m *MockClient) SendMessage(ctx context.Context, peerID int64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMsgs = append(m.sentMsgs, SentRecord{
		PeerID: peerID,
		Text:   text,
		SentAt: time.Now(),
	})
	slog.Info("mock telegram message sent", "peer_id", peerID, "text", text)
	return nil
}

func (m *MockClient) GetChat(ctx context.Context, chatID int64) (*ChatInfo, error) {
	return &ChatInfo{
		ID:       chatID,
		Title:    fmt.Sprintf("Mock Chat %d", chatID),
		Username: "mock_marketplace",
		Type:     "channel",
	}, nil
}

func (m *MockClient) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// InjectMessage simulates receiving an incoming Telegram message
func (m *MockClient) InjectMessage(ctx context.Context, msg *domain.TelegramMessage) error {
	m.mu.RLock()
	h := m.handler
	m.mu.RUnlock()

	if h != nil {
		return h(ctx, msg)
	}
	return nil
}

func (m *MockClient) GetSentMessages() []SentRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]SentRecord, len(m.sentMsgs))
	copy(copied, m.sentMsgs)
	return copied
}
