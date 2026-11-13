package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

type UpdatesRepo struct {
	coll *mongo.Collection
}

func NewUpdatesRepo(db *DB) *UpdatesRepo {
	return &UpdatesRepo{
		coll: db.Database.Collection("telegram_updates"),
	}
}

func (r *UpdatesRepo) SaveUpdate(ctx context.Context, update *domain.TelegramUpdate) error {
	if update.ReceivedAt.IsZero() {
		update.ReceivedAt = time.Now()
	}
	_, err := r.coll.InsertOne(ctx, update)
	if err != nil {
		return fmt.Errorf("insert telegram update: %w", err)
	}
	return nil
}
