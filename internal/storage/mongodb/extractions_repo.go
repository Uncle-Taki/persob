package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type LLMExtractionsRepo struct {
	coll *mongo.Collection
}

func NewLLMExtractionsRepo(db *DB) *LLMExtractionsRepo {
	return &LLMExtractionsRepo{
		coll: db.Database.Collection("llm_extractions"),
	}
}

func (r *LLMExtractionsRepo) Save(ctx context.Context, ext *domain.LLMExtraction) error {
	if ext.ID.IsZero() {
		ext.ID = primitive.NewObjectID()
	}
	if ext.CreatedAt.IsZero() {
		ext.CreatedAt = time.Now().UTC()
	}
	_, err := r.coll.InsertOne(ctx, ext)
	if err != nil {
		return fmt.Errorf("insert llm extraction: %w", err)
	}
	return nil
}

func (r *LLMExtractionsRepo) GetByMessageID(ctx context.Context, messageID string) (*domain.LLMExtraction, error) {
	filter := bson.M{"message_id": messageID}
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})
	var ext domain.LLMExtraction
	err := r.coll.FindOne(ctx, filter, opts).Decode(&ext)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("find llm extraction: %w", err)
	}
	return &ext, nil
}
