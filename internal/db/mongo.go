package db

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	client   *mongo.Client
	database *mongo.Database
	once     sync.Once
)

func ConnectMongo(uri, dbName string) *mongo.Database {
	once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		opts := options.Client().
			ApplyURI(uri).
			SetMaxPoolSize(10).
			SetMinPoolSize(2)

		var err error
		client, err = mongo.Connect(opts)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to MongoDB")
		}

		if err = client.Ping(ctx, nil); err != nil {
			log.Fatal().Err(err).Msg("Failed to ping MongoDB")
		}

		database = client.Database(dbName)

		ensureIndexes(ctx, database)

		log.Info().Str("db", dbName).Msg("Connected to MongoDB")
	})

	return database
}

func DisconnectMongo() {
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from MongoDB")
		}
	}
}

func GetCollection(name string) *mongo.Collection {
	return database.Collection(name)
}

func ensureIndexes(ctx context.Context, db *mongo.Database) {
	queueCol := db.Collection("queues")
	queueIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
		{
			Keys:    bson.D{{Key: "join_code", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
	}
	if _, err := queueCol.Indexes().CreateMany(ctx, queueIndexes); err != nil {
		log.Fatal().Err(err).Msg("Failed to create queue indexes")
	}

	entryCol := db.Collection("queue_entries")
	entryIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "token", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "queue_id", Value: 1}},
		},
	}
	if _, err := entryCol.Indexes().CreateMany(ctx, entryIndexes); err != nil {
		log.Fatal().Err(err).Msg("Failed to create queue_entry indexes")
	}

	hostCol := db.Collection("hosts")
	for _, indexName := range []string{"email_1", "phone_1", "public_id_1", "social_auth_provider_user_id_1", "social_auth_google_provider_user_id_1", "social_auth_apple_provider_user_id_1"} {
		if err := hostCol.Indexes().DropOne(ctx, indexName); err != nil {
			log.Warn().Err(err).Str("index", indexName).Msg("Failed to drop legacy host index")
		}
	}

	hostIndexes := []mongo.IndexModel{
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
	if _, err := hostCol.Indexes().CreateMany(ctx, hostIndexes); err != nil {
		log.Fatal().Err(err).Msg("Failed to create host indexes")
	}

	log.Info().Msg("MongoDB indexes ensured")
}
