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

	"github.com/google/uuid"
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
	plansCol := db.Collection("billing_plans")

	// Always re-seed to ensure latest tiers/slugs
	_ = plansCol.Drop(ctx)

	plans := []interface{}{
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "free-v1",
			Tier:         "free",
			Priority:     0,
			Name:         "Free Forever",
			Description:  "Perfect for small shops and individuals starting out.",
			IsFree:       true,
			CountryCode:  "",
			Currency:     "INR",
			MonthlyPrice: 0,
			YearlyPrice:  0,
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    1,
				MaxGuestsPerQueue:    25,
				HistoryAccess:        false,
				CustomBranding:       false,
				CanExport:            false,
				QueueExpiryHours:     24,
				CanViewGuestData:     false,
				HistoryRetentionDays: 7,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "pro-india",
			Tier:         "pro",
			Priority:     1,
			Name:         "Pro",
			Description:  "Advanced tools for growing teams and multiple queues.",
			IsFree:       false,
			CountryCode:  "IN",
			Currency:     "INR",
			MonthlyPrice: 49900,  // ₹499.00
			YearlyPrice:  549900, // ₹5499.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    25,
				MaxGuestsPerQueue:    100,
				HistoryAccess:        true,
				CustomBranding:       false,
				CanExport:            true,
				QueueExpiryHours:     72, // 3 days
				CanViewGuestData:     true,
				HistoryRetentionDays: 30,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "pro-global",
			Tier:         "pro",
			Priority:     1,
			Name:         "Pro",
			Description:  "Advanced tools for growing teams and multiple queues.",
			IsFree:       false,
			CountryCode:  "GLOBAL",
			Currency:     "USD",
			MonthlyPrice: 900,   // $9.00
			YearlyPrice:  10900, // $109.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    25,
				MaxGuestsPerQueue:    100,
				HistoryAccess:        true,
				CustomBranding:       false,
				CanExport:            true,
				QueueExpiryHours:     72,
				CanViewGuestData:     true,
				HistoryRetentionDays: 30,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "business-elite-india",
			Tier:         "elite",
			Priority:     2,
			Name:         "Business Elite",
			Description:  "Full-scale solution for high-traffic businesses and brands.",
			IsFree:       false,
			CountryCode:  "IN",
			Currency:     "INR",
			MonthlyPrice: 149900,  // ₹1,499.00
			YearlyPrice:  1649900, // ₹16,499.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    0,
				MaxGuestsPerQueue:    0,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     168, // 1 week
				CanViewGuestData:     true,
				HistoryRetentionDays: 0, // Unlimited
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "business-elite-global",
			Tier:         "elite",
			Priority:     2,
			Name:         "Business Elite",
			Description:  "Full-scale solution for high-traffic businesses and brands.",
			IsFree:       false,
			CountryCode:  "GLOBAL",
			Currency:     "USD",
			MonthlyPrice: 2900, // $29.00
			YearlyPrice:  29000,
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    0,
				MaxGuestsPerQueue:    0,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     168,
				CanViewGuestData:     true,
				HistoryRetentionDays: 0, // Unlimited
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "enterprise",
			Tier:         "enterprise",
			Priority:     3,
			Name:         "Enterprise",
			Description:  "Custom limits and dedicated support for large organizations.",
			IsFree:       false,
			CountryCode:  "",
			Currency:     "USD",
			MonthlyPrice: 0,
			YearlyPrice:  0,
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    0,
				MaxGuestsPerQueue:    0,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     720,
				CanViewGuestData:     true,
				HistoryRetentionDays: 0,
			},
			CreatedAt: time.Now(),
		},
	}

	_, err := plansCol.InsertMany(ctx, plans)
	if err != nil {
		log.Fatal("❌ Failed to seed plans:", err)
	}
	log.Println("✨ Seeded 6 comprehensive plans.")
}

func seedSettings(ctx context.Context, db *mongodriver.Database) {
	col := db.Collection("system_settings")

	count, _ := col.CountDocuments(ctx, bson.M{"slug": "global"})
	if count > 0 {
		log.Println("⏩ Settings already exist, skipping...")
		return
	}

	// Find the free plan to get its generated ID
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
	log.Println("✨ Seeded global app settings.")
}
