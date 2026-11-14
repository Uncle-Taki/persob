package telegram

import (
	"context"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/domain"
)

type IngestionAdapter interface {
	AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error)
}

type BasicGroupAdapter struct{}

func (a *BasicGroupAdapter) AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error) {
	senderID, username, displayName := extractSender(e, msg.FromID)
	var replyToID *int64
	if replyHeader, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok && replyHeader.ReplyToMsgID != 0 {
		id := int64(replyHeader.ReplyToMsgID)
		replyToID = &id
	}

	peerID := extractPeerID(msg.PeerID)
	if source != nil && source.TelegramPeerID != 0 {
		peerID = source.TelegramPeerID
	}

	sourceID := ""
	if source != nil {
		sourceID = source.ID.String()
	}

	return &domain.TelegramMessage{
		SourceID:          sourceID,
		TelegramPeerID:    peerID,
		TelegramChatID:    peerID,
		TelegramMessageID: int64(msg.ID),
		ChatType:          "group",
		SourceType:        string(domain.TelegramSourceTypeBasicGroup),
		SenderID:          senderID,
		SenderType:        senderType(msg.FromID),
		SenderUsername:    username,
		SenderDisplayName: displayName,
		MessageText:       msg.Message,
		MediaMetadata:     extractMedia(msg.Media),
		ReplyToMessageID:  replyToID,
		MessageDate:       time.Unix(int64(msg.Date), 0),
		ReceivedAt:        time.Now().UTC(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}, nil
}

type SupergroupAdapter struct{}

func (a *SupergroupAdapter) AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error) {
	senderID, username, displayName := extractSender(e, msg.FromID)
	var replyToID *int64
	if replyHeader, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok && replyHeader.ReplyToMsgID != 0 {
		id := int64(replyHeader.ReplyToMsgID)
		replyToID = &id
	}

	peerID := extractPeerID(msg.PeerID)
	if source != nil && source.TelegramPeerID != 0 {
		peerID = source.TelegramPeerID
	}

	sourceID := ""
	if source != nil {
		sourceID = source.ID.String()
	}

	return &domain.TelegramMessage{
		SourceID:          sourceID,
		TelegramPeerID:    peerID,
		TelegramChatID:    peerID,
		TelegramMessageID: int64(msg.ID),
		ChatType:          "supergroup",
		SourceType:        string(domain.TelegramSourceTypeSupergroup),
		SenderID:          senderID,
		SenderType:        senderType(msg.FromID),
		SenderUsername:    username,
		SenderDisplayName: displayName,
		MessageText:       msg.Message,
		MediaMetadata:     extractMedia(msg.Media),
		ReplyToMessageID:  replyToID,
		MessageDate:       time.Unix(int64(msg.Date), 0),
		ReceivedAt:        time.Now().UTC(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}, nil
}

type ForumAdapter struct{}

func (a *ForumAdapter) AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error) {
	senderID, username, displayName := extractSender(e, msg.FromID)
	var replyToID *int64
	var topicID *int64

	if replyHeader, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
		if replyHeader.ReplyToMsgID != 0 {
			id := int64(replyHeader.ReplyToMsgID)
			replyToID = &id
		}
		// In forums, topic root message ID is in ReplyToTopID (or ReplyToMsgID if top is 0)
		if replyHeader.ForumTopic {
			if replyHeader.ReplyToTopID != 0 {
				tid := int64(replyHeader.ReplyToTopID)
				topicID = &tid
			} else if replyHeader.ReplyToMsgID != 0 {
				tid := int64(replyHeader.ReplyToMsgID)
				topicID = &tid
			}
		} else if replyHeader.ReplyToTopID != 0 {
			tid := int64(replyHeader.ReplyToTopID)
			topicID = &tid
		}
	}

	peerID := extractPeerID(msg.PeerID)
	if source != nil && source.TelegramPeerID != 0 {
		peerID = source.TelegramPeerID
	}

	sourceID := ""
	if source != nil {
		sourceID = source.ID.String()
	}

	return &domain.TelegramMessage{
		SourceID:          sourceID,
		TelegramPeerID:    peerID,
		TelegramChatID:    peerID,
		TelegramMessageID: int64(msg.ID),
		TopicID:           topicID, // Never discard topic ID!
		ChatType:          "forum",
		SourceType:        string(domain.TelegramSourceTypeForum),
		SenderID:          senderID,
		SenderType:        senderType(msg.FromID),
		SenderUsername:    username,
		SenderDisplayName: displayName,
		MessageText:       msg.Message,
		MediaMetadata:     extractMedia(msg.Media),
		ReplyToMessageID:  replyToID,
		MessageDate:       time.Unix(int64(msg.Date), 0),
		ReceivedAt:        time.Now().UTC(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}, nil
}

type BroadcastChannelAdapter struct{}

func (a *BroadcastChannelAdapter) AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error) {
	// Channel posts do NOT represent messages from individual sellers.
	// Do NOT fabricate personal sender metadata!
	peerID := extractPeerID(msg.PeerID)
	if source != nil && source.TelegramPeerID != 0 {
		peerID = source.TelegramPeerID
	}

	sourceID := ""
	title := ""
	username := ""
	if source != nil {
		sourceID = source.ID.String()
		title = source.Title
		username = source.Username
	}

	authorDisplayName := title
	if msg.PostAuthor != "" {
		authorDisplayName = msg.PostAuthor
	}

	return &domain.TelegramMessage{
		SourceID:          sourceID,
		TelegramPeerID:    peerID,
		TelegramChatID:    peerID,
		TelegramMessageID: int64(msg.ID),
		ChatType:          "channel",
		SourceType:        string(domain.TelegramSourceTypeBroadcastChannel),
		SenderID:          0, // No fabricated personal sender ID
		SenderType:        "channel",
		SenderUsername:    username,
		SenderDisplayName: authorDisplayName,
		MessageText:       msg.Message,
		MediaMetadata:     extractMedia(msg.Media),
		MessageDate:       time.Unix(int64(msg.Date), 0),
		ReceivedAt:        time.Now().UTC(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}, nil
}

type LinkedDiscussionAdapter struct{}

func (a *LinkedDiscussionAdapter) AdaptMessage(ctx context.Context, e tg.Entities, msg *tg.Message, source *domain.TelegramSource) (*domain.TelegramMessage, error) {
	senderID, username, displayName := extractSender(e, msg.FromID)
	var replyToID *int64
	var linkedPostID *int64

	if replyHeader, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
		if replyHeader.ReplyToMsgID != 0 {
			id := int64(replyHeader.ReplyToMsgID)
			replyToID = &id
			linkedPostID = &id
		}
		if replyHeader.ReplyToTopID != 0 {
			top := int64(replyHeader.ReplyToTopID)
			linkedPostID = &top
		}
	}

	peerID := extractPeerID(msg.PeerID)
	sourceID := ""
	if source != nil {
		sourceID = source.ID.String()
	}

	return &domain.TelegramMessage{
		SourceID:          sourceID,
		TelegramPeerID:    peerID,
		TelegramChatID:    peerID,
		TelegramMessageID: int64(msg.ID),
		LinkedPostID:      linkedPostID, // Associates discussion thread to channel post
		ChatType:          "supergroup",
		SourceType:        "linked_discussion",
		SenderID:          senderID,
		SenderType:        senderType(msg.FromID),
		SenderUsername:    username,
		SenderDisplayName: displayName,
		MessageText:       msg.Message,
		MediaMetadata:     extractMedia(msg.Media),
		ReplyToMessageID:  replyToID,
		MessageDate:       time.Unix(int64(msg.Date), 0),
		ReceivedAt:        time.Now().UTC(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}, nil
}

func GetIngestionAdapter(sourceType domain.TelegramSourceType, isDiscussion bool) IngestionAdapter {
	if isDiscussion {
		return &LinkedDiscussionAdapter{}
	}
	switch sourceType {
	case domain.TelegramSourceTypeBasicGroup:
		return &BasicGroupAdapter{}
	case domain.TelegramSourceTypeSupergroup:
		return &SupergroupAdapter{}
	case domain.TelegramSourceTypeForum:
		return &ForumAdapter{}
	case domain.TelegramSourceTypeBroadcastChannel:
		return &BroadcastChannelAdapter{}
	default:
		return &SupergroupAdapter{}
	}
}

func extractSender(e tg.Entities, from tg.PeerClass) (int64, string, string) {
	user, ok := from.(*tg.PeerUser)
	if !ok {
		return 0, "", ""
	}
	id := user.UserID
	if u, ok := e.Users[id]; ok {
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		return id, u.Username, name
	}
	return id, "", ""
}

func senderType(from tg.PeerClass) string {
	switch from.(type) {
	case *tg.PeerUser:
		return "user"
	case *tg.PeerChannel:
		return "channel"
	case *tg.PeerChat:
		return "group"
	default:
		return "unknown"
	}
}

func extractMedia(media tg.MessageMediaClass) map[string]interface{} {
	if media == nil {
		return nil
	}
	meta := make(map[string]interface{})
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		meta["type"] = "photo"
		meta["has_photo"] = true
		if p, ok := m.Photo.(*tg.Photo); ok {
			meta["photo_id"] = p.ID
		}
	case *tg.MessageMediaDocument:
		meta["type"] = "document"
		if d, ok := m.Document.(*tg.Document); ok {
			meta["mime_type"] = d.MimeType
			meta["size"] = d.Size
		}
	default:
		meta["type"] = "other"
	}
	return meta
}
