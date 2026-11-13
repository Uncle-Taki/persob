package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/redis/go-redis/v9"
)

type Producer struct {
	client    *Client
	streamKey string
}

func NewProducer(client *Client, streamKey string) *Producer {
	return &Producer{
		client:    client,
		streamKey: streamKey,
	}
}

type StreamPayload struct {
	TelegramChatID    int64     `json:"telegram_chat_id"`
	TelegramMessageID int64     `json:"telegram_message_id"`
	ChatTitle         string    `json:"chat_title,omitempty"`
	ChatUsername      string    `json:"chat_username,omitempty"`
	SenderID          int64     `json:"sender_id"`
	SenderUsername    string    `json:"sender_username,omitempty"`
	SenderDisplayName string    `json:"sender_display_name,omitempty"`
	MessageText       string    `json:"message_text"`
	MessageDate       time.Time `json:"message_date"`
	ReceivedAt        time.Time `json:"received_at"`
}

func (p *Producer) PublishMessage(ctx context.Context, msg *domain.TelegramMessage) (string, error) {
	payload := StreamPayload{
		TelegramChatID:    msg.TelegramChatID,
		TelegramMessageID: msg.TelegramMessageID,
		ChatTitle:         msg.ChatTitle,
		ChatUsername:      msg.ChatUsername,
		SenderID:          msg.SenderID,
		SenderUsername:    msg.SenderUsername,
		SenderDisplayName: msg.SenderDisplayName,
		MessageText:       msg.MessageText,
		MessageDate:       msg.MessageDate,
		ReceivedAt:        msg.ReceivedAt,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal stream payload: %w", err)
	}

	res, err := p.client.Rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamKey,
		Values: map[string]interface{}{
			"payload": data,
			"chat_id": msg.TelegramChatID,
			"msg_id":  msg.TelegramMessageID,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("xadd message to stream: %w", err)
	}

	return res, nil
}
