package service

import (
	"context"
	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/system/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type SystemService struct {
	cfg         *config.Config
	settingsCol *mongodriver.Collection
}

func NewSystemService(cfg *config.Config, db *mongodriver.Database) *SystemService {
	return &SystemService{
		cfg:         cfg,
		settingsCol: db.Collection("system_settings"),
	}
}

func (s *SystemService) GetSettings(ctx context.Context) (*domain.SystemSettings, error) {
	var settings domain.SystemSettings
	err := s.settingsCol.FindOne(ctx, bson.M{"_id": "global"}).Decode(&settings)
	if err != nil && err != mongodriver.ErrNoDocuments {
		return nil, err
	}

	if err == mongodriver.ErrNoDocuments {
		// Return defaults if nothing in DB yet
		return &domain.SystemSettings{
			DefaultPlanID:                           "free",
			DefaultQueueJoinCodeLength:              6,
			DefaultQueueMaxJoinCodeAttempts:         5,
			DefaultQueueEntryIdleTimeoutMin:         30,
			DefaultQueueEntryGraceTimerSec:          120,
			DefaultQueueEntryRepositionOffset:       2,
			DefaultQueueSubscriptionGracePeriodDays: 3,
			SupportEmail:                            s.cfg.SupportEmail,
		}, nil
	}

	return &settings, nil
}

func (s *SystemService) UpdateSettings(ctx context.Context, settings *domain.SystemSettings) error {
	settings.ID = "global"
	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.settingsCol.UpdateOne(ctx, bson.M{"_id": "global"}, bson.M{"$set": settings}, opts)
	return err
}
