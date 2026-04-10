package mongo

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/log"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
)

var (
	client   *mongodriver.Client
	database *mongodriver.Database
	once     sync.Once
)

func Connect(uri, dbName string) *mongodriver.Database {
	once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cs, err := connstring.ParseAndValidate(uri)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to parse MongoDB URI")
		}

		mongoDbName := cs.Database
		if mongoDbName == "" {
			mongoDbName = dbName
		}

		opts := options.Client().
			ApplyURI(uri).
			SetMaxPoolSize(10).
			SetMinPoolSize(2)

		client, err = mongodriver.Connect(opts)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to MongoDB")
		}

		if err = client.Ping(ctx, nil); err != nil {
			log.Fatal().Err(err).Msg("Failed to ping MongoDB")
		}

		database = client.Database(mongoDbName)
		if err := EnsureIndexes(ctx, database); err != nil {
			log.Fatal().Err(err).Msg("Failed to ensure MongoDB indexes")
		}

		log.Info().Str("db", mongoDbName).Msg("Connected to MongoDB")
	})

	return database
}

func Disconnect() {
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from MongoDB")
		}
	}
}
