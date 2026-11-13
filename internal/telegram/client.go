package telegram

import (
	"context"

	"github.com/perso/perso-mind/internal/domain"
)

type ChatInfo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Username string `json:"username,omitempty"`
	Type     string `json:"type"`
}

type MessageHandler func(ctx context.Context, msg *domain.TelegramMessage) error

// TelegramClient abstracts MTProto, Bot API, or Business Bot adapters
type TelegramClient interface {
	Start(ctx context.Context) error
	Stop() error
	SetMessageHandler(handler MessageHandler)
	SendMessage(ctx context.Context, peerID int64, text string) error
	GetChat(ctx context.Context, chatID int64) (*ChatInfo, error)
	IsConnected() bool
}
