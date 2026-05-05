package mongo

import (
	"context"

	hostservice "queuebuzz/internal/modules/host/service"
	queueservice "queuebuzz/internal/modules/queue/service"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func EnsureIndexes(ctx context.Context, db *mongodriver.Database) error {
	if err := queueservice.EnsureQueueIndexes(ctx, db); err != nil {
		return err
	}
	if err := queueservice.EnsureEntryIndexes(ctx, db); err != nil {
		return err
	}
	if err := hostservice.EnsureIndexes(ctx, db); err != nil {
		return err
	}
	return nil
}
