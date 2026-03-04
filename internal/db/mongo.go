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
	// Queue indexes
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

	// QueueEntry indexes
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

	// Host indexes
	hostCol := db.Collection("hosts")
	hostIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "email", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "phone", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "public_id", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetSparse(true),
		},
	}
	if _, err := hostCol.Indexes().CreateMany(ctx, hostIndexes); err != nil {
		log.Fatal().Err(err).Msg("Failed to create host indexes")
	}

	log.Info().Msg("MongoDB indexes ensured")
}
