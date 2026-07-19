package service

import (
	"context"

	"queuebuzz/internal/constants"

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
		{
			Keys: bson.D{{Key: "_id", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "slug", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "join_code", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "host_public_id", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "host_id", Value: 1}, {Key: "created_at", Value: -1}},
		},
	}
	_, err := queueCol.Indexes().CreateMany(ctx, queueIndexes)
	return err
}

func EnsureEntryIndexes(ctx context.Context, db *mongodriver.Database) error {
	entryCol := db.Collection("queue_entries")

	// Drop the existing index to avoid conflicts when updating definition/options
	_ = entryCol.Indexes().DropOne(ctx, "queue_id_verify_code_active_unique")

	entryIndexes := []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: "token", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "queue_id", Value: 1},
				{Key: "verify_code", Value: 1},
			},
			Options: options.Index().
				SetName("queue_id_verify_code_active_unique").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{
					{
						Key: "status",
						Value: bson.D{{
							Key: "$in",
							Value: bson.A{
								constants.EntryStatusWaiting,
								constants.EntryStatusCalled,
								constants.EntryStatusIdle,
								constants.EntryStatusArrived,
							},
						}},
					},
					{
						Key: "verify_code",
						Value: bson.D{{
							Key:   "$gt",
							Value: "",
						}},
					},
				}),
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}, {Key: "status", Value: 1}, {Key: "email", Value: 1}},
			Options: options.Index().SetPartialFilterExpression(bson.D{{
				Key: "email",
				Value: bson.D{{
					Key:   "$gt",
					Value: "",
				}},
			}}),
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}, {Key: "status", Value: 1}, {Key: "phone", Value: 1}},
			Options: options.Index().SetPartialFilterExpression(bson.D{{
				Key: "phone",
				Value: bson.D{{
					Key:   "$gt",
					Value: "",
				}},
			}}),
		},
		{
			Keys: bson.D{{Key: "identity_hash", Value: 1}},
		},
	}
	_, err := entryCol.Indexes().CreateMany(ctx, entryIndexes)
	return err
}
