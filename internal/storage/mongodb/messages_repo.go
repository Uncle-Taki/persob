package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MessagesRepo struct {
	coll *mongo.Collection
}

func NewMessagesRepo(db *DB) *MessagesRepo {
	return &MessagesRepo{
		coll: db.Database.Collection("telegram_messages"),
	}
}

// UpsertMessage keeps both first-seen and current text, with a stable peer/message identity.
func (r *MessagesRepo) UpsertMessage(ctx context.Context, msg *domain.TelegramMessage) error {
	if msg.ReceivedAt.IsZero() {
		msg.ReceivedAt = time.Now()
	}
	if msg.ProcessingStatus == "" {
		msg.ProcessingStatus = domain.ProcessingStatusPending
	}
	if msg.ProcessingVersion == 0 {
		msg.ProcessingVersion = 1
	}

	filter := bson.M{
		"telegram_chat_id":    msg.TelegramChatID,
		"telegram_message_id": msg.TelegramMessageID,
	}

	// Preserve first-seen text for old and new documents while making the current
	// text readable by workers after an edit. Pipeline updates avoid a read/write race.
	literal := func(v interface{}) bson.M { return bson.M{"$literal": v} }
	insertValues := bson.M{
		"source_id": msg.SourceID, "telegram_peer_id": msg.TelegramPeerID,
		"received_at": msg.ReceivedAt, "message_date": msg.MessageDate,
		"chat_type": msg.ChatType, "chat_title": msg.ChatTitle, "chat_username": msg.ChatUsername,
		"sender_id": msg.SenderID, "sender_username": msg.SenderUsername,
		"sender_display_name": msg.SenderDisplayName, "sender_type": msg.SenderType,
		"language": msg.Language,
	}
	firstSeen := bson.M{}
	for k, v := range insertValues {
		firstSeen[k] = bson.M{"$ifNull": bson.A{"$" + k, literal(v)}}
	}
	firstSeen["original_message_text"] = bson.M{"$ifNull": bson.A{"$original_message_text", bson.M{"$ifNull": bson.A{"$message_text", literal(msg.MessageText)}}}}
	current := bson.M{
		"message_text": literal(msg.MessageText), "normalized_text": literal(msg.NormalizedText),
		"processing_status": literal(msg.ProcessingStatus), "processing_version": literal(msg.ProcessingVersion),
		"topic_id": literal(msg.TopicID), "linked_post_id": literal(msg.LinkedPostID),
		"source_type": literal(msg.SourceType), "reply_to_message_id": literal(msg.ReplyToMessageID),
		"media_metadata": literal(msg.MediaMetadata), "message_entities": literal(msg.MessageEntities),
	}
	if msg.EditedDate != nil {
		current["edited_date"] = literal(msg.EditedDate)
	}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: firstSeen}}, bson.D{{Key: "$set", Value: current}}}

	opts := options.Update().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("upsert telegram message: %w", err)
	}
	return nil
}

func (r *MessagesRepo) GetByChatAndMessageID(ctx context.Context, chatID, messageID int64) (*domain.TelegramMessage, error) {
	filter := bson.M{
		"telegram_chat_id":    chatID,
		"telegram_message_id": messageID,
	}
	var msg domain.TelegramMessage
	err := r.coll.FindOne(ctx, filter).Decode(&msg)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("find telegram message: %w", err)
	}
	return &msg, nil
}

func (r *MessagesRepo) UpdateProcessingStatus(ctx context.Context, chatID, messageID int64, status domain.ProcessingStatus) error {
	filter := bson.M{
		"telegram_chat_id":    chatID,
		"telegram_message_id": messageID,
	}
	update := bson.M{
		"$set": bson.M{
			"processing_status": status,
		},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update)
	return err
}

func (r *MessagesRepo) CountRecentProcessed(ctx context.Context, since time.Time) (int64, error) {
	filter := bson.M{
		"received_at":       bson.M{"$gte": since},
		"processing_status": domain.ProcessingStatusProcessed,
	}
	return r.coll.CountDocuments(ctx, filter)
}

func (r *MessagesRepo) GetPendingMessages(ctx context.Context, limit int64) ([]domain.TelegramMessage, error) {
	filter := bson.M{"processing_status": domain.ProcessingStatusPending}
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "received_at", Value: 1}})

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find pending messages: %w", err)
	}
	defer cursor.Close(ctx)

	var msgs []domain.TelegramMessage
	if err := cursor.All(ctx, &msgs); err != nil {
		return nil, fmt.Errorf("decode pending messages: %w", err)
	}
	return msgs, nil
}
