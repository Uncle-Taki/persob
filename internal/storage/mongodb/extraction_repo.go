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

type ExtractionRepo struct {
	coll *mongo.Collection
}

func NewExtractionRepo(db *DB) *ExtractionRepo {
	return &ExtractionRepo{
		coll: db.Database.Collection("extracted_messages"),
	}
}

func (r *ExtractionRepo) SaveExtraction(ctx context.Context, ext *domain.ExtractedMessage) error {
	if ext.CreatedAt.IsZero() {
		ext.CreatedAt = time.Now()
	}
	filter := bson.M{"message_id": ext.MessageID}
	opts := options.Replace().SetUpsert(true)
	_, err := r.coll.ReplaceOne(ctx, filter, ext, opts)
	if err != nil {
		return fmt.Errorf("upsert extraction result: %w", err)
	}
	return nil
}

func (r *ExtractionRepo) GetByMessageID(ctx context.Context, messageID string) (*domain.ExtractedMessage, error) {
	filter := bson.M{"message_id": messageID}
	var ext domain.ExtractedMessage
	err := r.coll.FindOne(ctx, filter).Decode(&ext)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("find extraction: %w", err)
	}
	return &ext, nil
}
