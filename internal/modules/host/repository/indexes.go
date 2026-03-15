package repository

import (
	"context"

	"queuebuzz/internal/log"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func EnsureIndexes(ctx context.Context, db *mongodriver.Database) error {
	hostCol := db.Collection("hosts")
	for _, indexName := range []string{"email_1", "phone_1", "public_id_1", "social_auth_provider_user_id_1", "social_auth_google_provider_user_id_1", "social_auth_apple_provider_user_id_1"} {
		if err := hostCol.Indexes().DropOne(ctx, indexName); err != nil {
			log.Warn().Err(err).Str("index", indexName).Msg("Failed to drop legacy host index")
		}
	}

	hostIndexes := []mongodriver.IndexModel{
		{
			Keys: bson.D{{Key: "email", Value: 1}},
			Options: options.Index().
				SetName("email_1").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "email", Value: bson.D{{Key: "$type", Value: "string"}}}}),
		},
		{
			Keys: bson.D{{Key: "phone", Value: 1}},
			Options: options.Index().
				SetName("phone_1").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "phone", Value: bson.D{{Key: "$type", Value: "string"}}}}),
		},
		{
			Keys: bson.D{{Key: "public_id", Value: 1}},
			Options: options.Index().
				SetName("public_id_1").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "social_auth.google.provider_user_id", Value: 1}},
			Options: options.Index().
				SetName("social_auth_google_provider_user_id_1").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "social_auth.google.provider_user_id", Value: bson.D{{Key: "$type", Value: "string"}}}}),
		},
		{
			Keys: bson.D{{Key: "social_auth.apple.provider_user_id", Value: 1}},
			Options: options.Index().
				SetName("social_auth_apple_provider_user_id_1").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "social_auth.apple.provider_user_id", Value: bson.D{{Key: "$type", Value: "string"}}}}),
		},
	}
	_, err := hostCol.Indexes().CreateMany(ctx, hostIndexes)
	return err
}
