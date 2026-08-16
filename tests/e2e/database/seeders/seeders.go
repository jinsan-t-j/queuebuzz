package seeders

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func SeedDefaults(db *mongo.Database) {
	ctx := context.Background()
	_, _ = db.Collection("billing_plans").InsertOne(ctx, map[string]any{
		"_id":           "free-v1",
		"slug":          "free-v1",
		"tier":          "free",
		"name":          "Free Forever",
		"is_free":       true,
		"country_code":  "",
		"currency":      "INR",
		"monthly_price": 0,
		"yearly_price":  0,
		"limits": map[string]any{
			"max_queues_per_month":   1,
			"max_guests_per_queue":   25,
			"history_access":         false,
			"custom_branding":        false,
			"can_export":             false,
			"queue_expiry_hours":     24,
			"can_view_guest_data":    false,
			"history_retention_days": 7,
			"allow_geo_lock":         false,
		},
	})

	_, _ = db.Collection("billing_plans").InsertOne(ctx, map[string]any{
		"_id":                         "pro-in-v1",
		"slug":                        "pro-india",
		"tier":                        "pro",
		"name":                        "Pro",
		"is_free":                     false,
		"country_code":                "IN",
		"currency":                    "INR",
		"monthly_price":               49900,
		"yearly_price":                549900,
		"provider_monthly_product_id": "prod_pro_monthly",
		"limits": map[string]any{
			"max_queues_per_month":   25,
			"max_guests_per_queue":   100,
			"history_access":         true,
			"custom_branding":        false,
			"can_export":             true,
			"queue_expiry_hours":     72,
			"can_view_guest_data":    true,
			"history_retention_days": 30,
			"allow_geo_lock":         true,
		},
	})

	_, _ = db.Collection("billing_plans").InsertOne(ctx, map[string]any{
		"_id":                         "pro-us-v1",
		"slug":                        "pro-us",
		"tier":                        "pro",
		"name":                        "Pro US",
		"is_free":                     false,
		"country_code":                "US",
		"currency":                    "USD",
		"monthly_price":               1900,
		"yearly_price":                19900,
		"provider_monthly_product_id": "prod_pro_us_monthly",
		"limits": map[string]any{
			"max_queues_per_month":   25,
			"max_guests_per_queue":   100,
			"history_access":         true,
			"custom_branding":        false,
			"can_export":             true,
			"queue_expiry_hours":     72,
			"can_view_guest_data":    true,
			"history_retention_days": 30,
			"allow_geo_lock":         true,
		},
	})

	_, _ = db.Collection("billing_plans").InsertOne(ctx, map[string]any{
		"_id":                         "elite-v1",
		"slug":                        "elite",
		"tier":                        "pro",
		"name":                        "Elite",
		"is_free":                     false,
		"country_code":                "",
		"currency":                    "INR",
		"monthly_price":               99900,
		"yearly_price":                1099900,
		"provider_monthly_product_id": "prod_elite_monthly",
		"trial_enabled":               true,
		"trial_duration_days":         3,
		"trial_access_token":          "test-trial-token",
		"limits": map[string]any{
			"max_queues_per_month":   100,
			"max_guests_per_queue":   500,
			"history_access":         true,
			"custom_branding":        true,
			"can_export":             true,
			"queue_expiry_hours":     168,
			"can_view_guest_data":    true,
			"history_retention_days": 365,
			"allow_geo_lock":         true,
		},
	})

	_, _ = db.Collection("system_settings").InsertOne(ctx, map[string]any{
		"_id":                                  "global",
		"slug":                                 "global",
		"default_plan_id":                      "free-v1",
		"support_email":                        "support@test.com",
		"default_queue_join_code_length":       6,
		"default_queue_max_join_code_attempts": 5,
		"default_queue_entry_idle_timeout_min": 10,
	})
}
