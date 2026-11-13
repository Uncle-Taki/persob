package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ConversationRepo struct {
	coll *mongo.Collection
}

func NewConversationRepo(db *DB) *ConversationRepo {
	return &ConversationRepo{
		coll: db.Database.Collection("conversation_messages"),
	}
}

func (r *ConversationRepo) SaveMessage(ctx context.Context, msg *domain.ConversationMessage) error {
	if msg.SentAt.IsZero() {
		msg.SentAt = time.Now()
	}
	_, err := r.coll.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("save conversation message: %w", err)
	}
	return nil
}

func (r *ConversationRepo) GetMessagesByConversationID(ctx context.Context, convID string) ([]domain.ConversationMessage, error) {
	filter := bson.M{"conversation_id": convID}
	opts := options.Find().SetSort(bson.D{{Key: "sent_at", Value: 1}})

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find conversation messages: %w", err)
	}
	defer cursor.Close(ctx)

	var msgs []domain.ConversationMessage
	if err := cursor.All(ctx, &msgs); err != nil {
		return nil, fmt.Errorf("decode conversation messages: %w", err)
	}
	return msgs, nil
}
