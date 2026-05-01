package main

import (
	"context"
	"log"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/billing/domain"
	systemdomain "queuebuzz/internal/modules/system/domain"
	"queuebuzz/internal/mongo"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func main() {
	cfg := config.Load()
	_, db := mongo.Connect(cfg.DBUri, cfg.DBName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	seedPlans(ctx, db)
	seedSettings(ctx, db)

	log.Println("✅ Database seeding completed successfully.")
}

func seedPlans(ctx context.Context, db *mongodriver.Database) {
	col := db.Collection("billing_plans")

	// Check if plans already exist
	count, _ := col.CountDocuments(ctx, bson.M{})
	if count > 0 {
		log.Println("⏩ Plans already exist, skipping...")
		return
	}

	plans := []interface{}{
		domain.Plan{
			ID:           "free-v1",
			Name:         "Free Starter",
			Description:  "Perfect for small kiosks and clinics.",
			IsFree:       true,
			CountryCode:  "GLOBAL",
			Currency:     "USD",
			MonthlyPrice: 0,
			YearlyPrice:  0,
			Limits: domain.PlanLimit{
				MaxQueues:            1,
				MaxGuests:            20,
				HistoryAccess:        false,
				CustomBranding:       false,
				CanExport:            false,
				QueueExpiryHours:     12,
				HistoryRetentionDays: 7,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           "premium-india",
			Name:         "Premium (India)",
			Description:  "Unlimited queues and advanced analytics.",
			IsFree:       false,
			CountryCode:  "IN",
			Currency:     "INR",
			MonthlyPrice: 99900, // ₹999.00
			YearlyPrice:  999000,
			Limits: domain.PlanLimit{
				MaxQueues:            10,
				MaxGuests:            500,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     168, // 1 week
				HistoryRetentionDays: 365,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           "premium-global",
			Name:         "Premium (Global)",
			Description:  "Unlimited queues and advanced analytics.",
			IsFree:       false,
			CountryCode:  "GLOBAL",
			Currency:     "USD",
			MonthlyPrice: 1900, // $19.00
			YearlyPrice:  19000,
			Limits: domain.PlanLimit{
				MaxQueues:            10,
				MaxGuests:            500,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     168,
				HistoryRetentionDays: 365,
			},
			CreatedAt: time.Now(),
		},
	}

	_, err := col.InsertMany(ctx, plans)
	if err != nil {
		log.Fatal("❌ Failed to seed plans:", err)
	}
	log.Println("✨ Seeded 3 base plans.")
}

func seedSettings(ctx context.Context, db *mongodriver.Database) {
	col := db.Collection("system_settings")

	count, _ := col.CountDocuments(ctx, bson.M{"_id": "global"})
	if count > 0 {
		log.Println("⏩ Settings already exist, skipping...")
		return
	}

	settings := systemdomain.SystemSettings{
		ID:                                      "global",
		DefaultPlanID:                           "free-v1",
		DefaultQueueJoinCodeLength:              6,
		DefaultQueueMaxJoinCodeAttempts:         5,
		DefaultQueueEntryIdleTimeoutMin:         3,
		DefaultQueueEntryGraceTimerSec:          300,
		DefaultQueueEntryRepositionOffset:       3,
		DefaultQueueSubscriptionGracePeriodDays: 3,
		SupportEmail:                            "support@queuebuzz.com",
	}

	_, err := col.InsertOne(ctx, settings)
	if err != nil {
		log.Fatal("❌ Failed to seed settings:", err)
	}
	log.Println("✨ Seeded global app settings.")
}
