package mongo

import (
	"context"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/log"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
)

func Connect(cfg *config.Config) (*mongodriver.Client, *mongodriver.Database) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cs, err := connstring.ParseAndValidate(cfg.DBUri)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse MongoDB URI")
	}

	mongoDbName := cs.Database
	if mongoDbName == "" {
		mongoDbName = cfg.DBName
	}

	maxPoolSize := cfg.MongoDBMaxPoolSize
	if maxPoolSize == 0 || maxPoolSize > 100 {
		log.Warn().Uint64("configured", maxPoolSize).Msg("Capping MongoDB maxPoolSize to 100 to satisfy Oracle Autonomous Database / NoSQL connection limits")
		maxPoolSize = 100
	}

	opts := options.Client().
		ApplyURI(cfg.DBUri).
		SetMaxPoolSize(maxPoolSize).
		SetMinPoolSize(10).
		SetMaxConnecting(10).
		SetMaxConnIdleTime(60 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(10 * time.Second).
		SetBSONOptions(&options.BSONOptions{
			ObjectIDAsHexString: true,
		})

	client, err := mongodriver.Connect(opts)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to MongoDB")
	}

	if err = client.Ping(ctx, nil); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping MongoDB")
	}

	database := client.Database(mongoDbName)
	if err := EnsureIndexes(ctx, database); err != nil {
		log.Warn().Err(err).Msg("Could not create some MongoDB indexes (ignoring error for Oracle Autonomous Database compatibility)")
	}

	log.Info().Str("db", mongoDbName).Msg("Connected to MongoDB")
	return client, database
}
