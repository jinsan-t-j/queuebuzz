package service

import (
	"context"
	"errors"
	"queuebuzz/internal/log"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func EnsureIndexes(ctx context.Context, db *mongodriver.Database) error {
	hostCol := db.Collection("hosts")

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
	if err != nil {
		if isIndexConflict(err) {
			log.Info().Msg("Index conflict detected in 'hosts' collection. Re-creating indexes...")
			// Drop all indexes (except _id) and retry creation
			if dropErr := hostCol.Indexes().DropAll(ctx); dropErr != nil {
				log.Error().Err(dropErr).Msg("Failed to drop indexes after conflict")
				return dropErr
			}
			_, err = hostCol.Indexes().CreateMany(ctx, hostIndexes)
			return err
		}
		return err
	}

	return nil
}

func isIndexConflict(err error) bool {
	var ce mongodriver.CommandError
	if errors.As(err, &ce) {
		// 85: IndexOptionsConflict, 86: IndexKeySpecsConflict
		return ce.Code == 85 || ce.Code == 86
	}
	return false
}
