package seed

import (
	"context"
	"log"

	"queuebuzz/internal/modules/billing/domain"
	systemdomain "queuebuzz/internal/modules/system/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/google/uuid"
)

func seedSettings(ctx context.Context, db *mongodriver.Database) {
	col := db.Collection("system_settings")

	count, _ := col.CountDocuments(ctx, bson.M{"slug": "global"})
	if count > 0 {
		log.Println("Settings already exist, skipping...")
		return
	}

	var freePlan domain.Plan
	err := db.Collection("billing_plans").FindOne(ctx, bson.M{"slug": "free-v1"}).Decode(&freePlan)
	if err != nil {
		log.Fatal("❌ Failed to find free-v1 plan for settings:", err)
	}

	settings := systemdomain.SystemSettings{
		ID:                                      uuid.New().String(),
		Slug:                                    "global",
		DefaultPlanID:                           freePlan.ID,
		DefaultQueueJoinCodeLength:              6,
		DefaultQueueMaxJoinCodeAttempts:         5,
		DefaultQueueEntryIdleTimeoutMin:         3,
		DefaultQueueEntryGraceTimerSec:          300,
		DefaultQueueEntryRepositionOffset:       3,
		DefaultQueueSubscriptionGracePeriodDays: 3,
		SupportEmail:                            "support@queuebuzz.com",
	}

	_, err = col.InsertOne(ctx, settings)
	if err != nil {
		log.Fatal("❌ Failed to seed settings:", err)
	}
	log.Println("Seeded global app settings.")
}
