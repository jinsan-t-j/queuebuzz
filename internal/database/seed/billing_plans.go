package seed

import (
	"context"
	"log"
	"time"

	"queuebuzz/internal/modules/billing/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/google/uuid"
)

func seedPlans(ctx context.Context, db *mongodriver.Database) {
	plansCol := db.Collection("billing_plans")

	plans := []interface{}{
		domain.Plan{
			ID:           uuid.New().String(),
			Slug:         "free-v1",
			Tier:         "free",
			Priority:     0,
			Name:         "Free Forever",
			Description:  "Perfect for small shops and individuals starting out.",
			IsFree:       true,
			CountryCode:  "GLOBAL",
			Currency:     "",
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
			ID:                       uuid.New().String(),
			Slug:                     "pro-india",
			Tier:                     "pro",
			ProviderMonthlyProductID: "pdt_0Ne5uKsxY42uLuUmU2WKw",
			ProviderYearlyProductID:  "pdt_0Ne5uaCCG7EHWSNhqvm6y",
			Priority:                 1,
			Name:                     "Pro",
			Description:              "Advanced tools for growing teams and multiple queues.",
			IsFree:                   false,
			CountryCode:              "IN",
			Currency:                 "INR",
			MonthlyPrice:             49900,  // ₹499.00
			YearlyPrice:              549900, // ₹5499.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    25,
				MaxGuestsPerQueue:    100,
				HistoryAccess:        true,
				CustomBranding:       false,
				CanExport:            true,
				QueueExpiryHours:     72, // 3 days
				CanViewGuestData:     true,
				HistoryRetentionDays: 0,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:                       uuid.New().String(),
			Slug:                     "pro-global",
			Tier:                     "pro",
			ProviderMonthlyProductID: "pdt_0Ne5uofrnHT4jcxTHUnvj",
			ProviderYearlyProductID:  "pdt_0Ne5v3GEqWY2e0ZlKxcZT",
			Priority:                 1,
			Name:                     "Pro",
			Description:              "Advanced tools for growing teams and multiple queues.",
			IsFree:                   false,
			CountryCode:              "GLOBAL",
			Currency:                 "USD",
			MonthlyPrice:             900,   // $9.00
			YearlyPrice:              10900, // $109.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    25,
				MaxGuestsPerQueue:    100,
				HistoryAccess:        true,
				CustomBranding:       false,
				CanExport:            true,
				QueueExpiryHours:     72,
				CanViewGuestData:     true,
				HistoryRetentionDays: 0,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:                       uuid.New().String(),
			Slug:                     "business-elite-india",
			Tier:                     "elite",
			ProviderMonthlyProductID: "pdt_0Ne5wFV18Y9BvXsfH7V0h",
			ProviderYearlyProductID:  "pdt_0Ne5wWhuf8iEjEkRa3bH2",
			Priority:                 2,
			Name:                     "Business Elite",
			Description:              "Full-scale solution for high-traffic businesses and brands.",
			IsFree:                   false,
			CountryCode:              "IN",
			Currency:                 "INR",
			MonthlyPrice:             149900,  // ₹1,499.00
			YearlyPrice:              1649900, // ₹16,499.00
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    50,
				MaxGuestsPerQueue:    0,
				HistoryAccess:        true,
				CustomBranding:       true,
				CanExport:            true,
				QueueExpiryHours:     168, // 1 week
				CanViewGuestData:     true,
				HistoryRetentionDays: 0,
			},
			CreatedAt: time.Now(),
		},
		domain.Plan{
			ID:                       uuid.New().String(),
			Slug:                     "business-elite-global",
			Tier:                     "elite",
			ProviderMonthlyProductID: "pdt_0Ne5wrIFDOx2iHElcLfQU",
			ProviderYearlyProductID:  "pdt_0Ne5x7prwzv6qp16pDVMy",
			Priority:                 2,
			Name:                     "Business Elite",
			Description:              "Full-scale solution for high-traffic businesses and brands.",
			IsFree:                   false,
			CountryCode:              "GLOBAL",
			Currency:                 "USD",
			MonthlyPrice:             2900, // $29.00
			YearlyPrice:              29000,
			Limits: domain.PlanLimit{
				MaxQueuesPerMonth:    50,
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

	count, _ := plansCol.CountDocuments(ctx, bson.M{})
	if count > 0 {
		return
	}

	// Convert to interface slice for InsertMany
	plansInterfaces := make([]interface{}, len(plans))
	copy(plansInterfaces, plans)

	_, err := plansCol.InsertMany(ctx, plansInterfaces)
	if err != nil {
		log.Fatal("❌ Failed to seed plans:", err)
	}

	log.Println("Seeded initial billing plans.")
}
