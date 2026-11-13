package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ConversationEventsRepo struct {
	coll *mongo.Collection
}

func NewConversationEventsRepo(db *DB) *ConversationEventsRepo {
	return &ConversationEventsRepo{
		coll: db.Database.Collection("conversation_events"),
	}
}

func (r *ConversationEventsRepo) SaveEvent(ctx context.Context, event *domain.ConversationEvent) error {
	if event.ID.IsZero() {
		event.ID = primitive.NewObjectID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	_, err := r.coll.InsertOne(ctx, event)
	if err != nil {
		return fmt.Errorf("insert conversation event: %w", err)
	}
	return nil
}

func (r *ConversationEventsRepo) GetEventsByConversationID(ctx context.Context, convID string) ([]domain.ConversationEvent, error) {
	filter := bson.M{"conversation_id": convID}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find conversation events: %w", err)
	}
	defer cursor.Close(ctx)

	var events []domain.ConversationEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode conversation events: %w", err)
	}
	return events, nil
}
