package seed

import (
	"context"
	"log"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func Run(ctx context.Context, db *mongodriver.Database) {
	log.Println("Starting database seeding...")

	seedPlans(ctx, db)
	seedSettings(ctx, db)

	log.Println("✅ Database seeding completed successfully.")
}
