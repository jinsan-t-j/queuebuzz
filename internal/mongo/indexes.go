package mongo

import (
	"context"

	hostrepo "queuebuzz/internal/modules/host/repository"
	queuerepo "queuebuzz/internal/modules/queue/repository"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func EnsureIndexes(ctx context.Context, db *mongodriver.Database) error {
	if err := queuerepo.EnsureQueueIndexes(ctx, db); err != nil {
		return err
	}
	if err := queuerepo.EnsureEntryIndexes(ctx, db); err != nil {
		return err
	}
	if err := hostrepo.EnsureIndexes(ctx, db); err != nil {
		return err
	}
	return nil
}
