package telegram_test

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/telegram"
)

func TestForumAdapterPreservesTopicID(t *testing.T) {
	adapter := telegram.GetIngestionAdapter(domain.TelegramSourceTypeForum, false)

	topicIDExpected := int64(450)
	tgMsg := &tg.Message{
		ID:      1001,
		PeerID:  &tg.PeerChannel{ChannelID: 1987654321},
		FromID:  &tg.PeerUser{UserID: 888123},
		Message: "فروش مک بوک پرو ام ۳ تمیز",
		Date:    int(time.Now().Unix()),
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   true,
			ReplyToTopID: int(topicIDExpected),
			ReplyToMsgID: 1000,
		},
	}

	entities := tg.Entities{
		Users: map[int64]*tg.User{
			888123: {ID: 888123, FirstName: "Reza", Username: "reza_apple"},
		},
	}

	source := &domain.TelegramSource{
		TelegramPeerID: -1001987654321,
		SourceType:     domain.TelegramSourceTypeForum,
	}

	domainMsg, err := adapter.AdaptMessage(context.Background(), entities, tgMsg, source)
	if err != nil {
		t.Fatalf("AdaptMessage error: %v", err)
	}

	if domainMsg.TopicID == nil {
		t.Fatalf("Expected TopicID to be preserved, got nil")
	}
	if *domainMsg.TopicID != topicIDExpected {
		t.Errorf("TopicID = %d, want %d", *domainMsg.TopicID, topicIDExpected)
	}
	if domainMsg.SourceType != "forum" {
		t.Errorf("SourceType = %s, want forum", domainMsg.SourceType)
	}
	if domainMsg.SenderID != 888123 {
		t.Errorf("SenderID = %d, want 888123", domainMsg.SenderID)
	}
}

func TestBroadcastChannelAdapterDoesNotFabricateSenderMetadata(t *testing.T) {
	adapter := telegram.GetIngestionAdapter(domain.TelegramSourceTypeBroadcastChannel, false)

	tgMsg := &tg.Message{
		ID:         2002,
		PeerID:     &tg.PeerChannel{ChannelID: 1234567890},
		Post:       true,
		PostAuthor: "ادمین فروشگاه",
		Message:    "قیمت روز آیفون ۱۵ پرو",
		Date:       int(time.Now().Unix()),
	}

	entities := tg.Entities{}

	source := &domain.TelegramSource{
		TelegramPeerID: -1001234567890,
		Title:          "کانال رسمی بازار",
		Username:       "bazaar_official",
		SourceType:     domain.TelegramSourceTypeBroadcastChannel,
	}

	domainMsg, err := adapter.AdaptMessage(context.Background(), entities, tgMsg, source)
	if err != nil {
		t.Fatalf("AdaptMessage error: %v", err)
	}

	if domainMsg.SenderID != 0 {
		t.Errorf("SenderID = %d, want 0 (no personal sender metadata on broadcast channels)", domainMsg.SenderID)
	}
	if domainMsg.SenderType != "channel" {
		t.Errorf("SenderType = %s, want channel", domainMsg.SenderType)
	}
	if domainMsg.SenderDisplayName != "ادمین فروشگاه" {
		t.Errorf("SenderDisplayName = %s, want post author", domainMsg.SenderDisplayName)
	}
}

func TestBasicGroupAdapterDoesNotTreatChannelOrMissingSenderAsAUser(t *testing.T) {
	adapter := telegram.GetIngestionAdapter(domain.TelegramSourceTypeBasicGroup, false)

	tests := []struct {
		name     string
		from     tg.PeerClass
		wantID   int64
		wantType string
	}{
		{name: "anonymous channel admin", from: &tg.PeerChannel{ChannelID: 55}, wantType: "channel"},
		{name: "missing sender", wantType: "unknown"},
		{name: "user id without cached entity", from: &tg.PeerUser{UserID: 88}, wantID: 88, wantType: "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := adapter.AdaptMessage(context.Background(), tg.Entities{}, &tg.Message{
				ID: 4, PeerID: &tg.PeerChat{ChatID: 22}, FromID: tt.from,
				Message: "فروش کالا", Date: int(time.Now().Unix()),
			}, &domain.TelegramSource{TelegramPeerID: -22})
			if err != nil {
				t.Fatalf("AdaptMessage error: %v", err)
			}
			if msg.SenderID != tt.wantID {
				t.Errorf("SenderID = %d, want %d", msg.SenderID, tt.wantID)
			}
			if msg.SenderType != tt.wantType {
				t.Errorf("SenderType = %q, want %q", msg.SenderType, tt.wantType)
			}
			if msg.SenderUsername != "" || msg.SenderDisplayName != "" {
				t.Errorf("fabricated user metadata: username=%q display_name=%q", msg.SenderUsername, msg.SenderDisplayName)
			}
		})
	}
}

func TestLinkedDiscussionReplyDoesNotFabricateOriginalPostID(t *testing.T) {
	adapter := telegram.GetIngestionAdapter(domain.TelegramSourceTypeBroadcastChannel, true)

	linkedPostExpected := int64(1234)
	tgMsg := &tg.Message{
		ID:      3003,
		PeerID:  &tg.PeerChannel{ChannelID: 1234567899},
		FromID:  &tg.PeerUser{UserID: 999888},
		Message: "موجود دارید هنوز؟ قیمت آخر چنده؟",
		Date:    int(time.Now().Unix()),
		ReplyTo: &tg.MessageReplyHeader{
			ReplyToMsgID: int(linkedPostExpected),
		},
	}

	entities := tg.Entities{
		Users: map[int64]*tg.User{
			999888: {ID: 999888, FirstName: "Mohsen", Username: "mohsen_k"},
		},
	}

	source := &domain.TelegramSource{
		TelegramPeerID: -1001234567890,
	}

	domainMsg, err := adapter.AdaptMessage(context.Background(), entities, tgMsg, source)
	if err != nil {
		t.Fatalf("AdaptMessage error: %v", err)
	}

	if domainMsg.LinkedPostID != nil {
		t.Fatalf("discussion reply must not fabricate a channel post: %v", domainMsg.LinkedPostID)
	}
	if domainMsg.ReplyToMessageID == nil || *domainMsg.ReplyToMessageID != linkedPostExpected {
		t.Fatal("discussion reply identity must be retained")
	}
	if domainMsg.SourceType != "linked_discussion" {
		t.Errorf("SourceType = %s, want linked_discussion", domainMsg.SourceType)
	}
}
