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
	raw, err := bson.Marshal(ext)
	if err != nil {
		return err
	}
	var fields bson.M
	if err = bson.Unmarshal(raw, &fields); err != nil {
		return err
	}
	updates := bson.M{}
	for key, value := range fields {
		if key == "_id" {
			continue
		}
		updates[key] = bson.M{"$cond": bson.A{bson.M{"$gte": bson.A{ext.SourceRevisionAt, bson.M{"$ifNull": bson.A{"$source_revision_at", time.Unix(0, 0)}}}}, bson.M{"$literal": value}, "$" + key}}
	}
	opts := options.Update().SetUpsert(true)
	_, err = r.coll.UpdateOne(ctx, filter, mongo.Pipeline{bson.D{{Key: "$set", Value: updates}}}, opts)
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
