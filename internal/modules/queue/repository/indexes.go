package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func EnsureQueueIndexes(ctx context.Context, db *mongodriver.Database) error {
	queueCol := db.Collection("queues")
	queueIndexes := []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
		{
			Keys:    bson.D{{Key: "join_code", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "slug", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
	}
	_, err := queueCol.Indexes().CreateMany(ctx, queueIndexes)
	return err
}

func EnsureEntryIndexes(ctx context.Context, db *mongodriver.Database) error {
	entryCol := db.Collection("queue_entries")
	entryIndexes := []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: "token", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "identity_hash", Value: 1}},
		},
	}
	_, err := entryCol.Indexes().CreateMany(ctx, entryIndexes)
	return err
}
