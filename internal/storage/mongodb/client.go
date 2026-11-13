package mongodb

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

func Connect(ctx context.Context, uri, dbName string) (*DB, error) {
	clientOptions := options.Client().ApplyURI(uri)
	clientOptions.SetMaxPoolSize(50)
	clientOptions.SetMinPoolSize(5)
	clientOptions.SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	slog.Info("connected to mongodb successfully", "database", dbName)
	db := &DB{
		Client:   client,
		Database: client.Database(dbName),
	}

	// Initialize indexes
	if err := db.EnsureIndexes(ctx); err != nil {
		slog.Warn("failed to initialize some mongodb indexes", "error", err)
	}

	return db, nil
}

func (db *DB) Close(ctx context.Context) error {
	if db.Client != nil {
		return db.Client.Disconnect(ctx)
	}
	return nil
}

func (db *DB) EnsureIndexes(ctx context.Context) error {
	// telegram_messages indexes
	msgColl := db.Database.Collection("telegram_messages")
	_, err := msgColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "telegram_chat_id", Value: 1},
				{Key: "telegram_message_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "processing_status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "received_at", Value: -1}},
		},
	})
	if err != nil {
		return fmt.Errorf("create telegram_messages indexes: %w", err)
	}

	// extracted_messages indexes
	extColl := db.Database.Collection("extracted_messages")
	_, err = extColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "message_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "classification", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	})
	if err != nil {
		return fmt.Errorf("create extracted_messages indexes: %w", err)
	}

	// conversation_messages indexes
	convMsgColl := db.Database.Collection("conversation_messages")
	_, err = convMsgColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "conversation_id", Value: 1},
			{Key: "sent_at", Value: 1},
		},
	})
	if err != nil {
		return fmt.Errorf("create conversation_messages indexes: %w", err)
	}

	// processing_jobs indexes
	jobsColl := db.Database.Collection("processing_jobs")
	_, err = jobsColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "job_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
	})
	if err != nil {
		return fmt.Errorf("create processing_jobs indexes: %w", err)
	}

	return nil
}
