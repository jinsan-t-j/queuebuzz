package service

import (
	"context"
	"encoding/json"
	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/system/domain"
	internalredis "queuebuzz/internal/redis"
	"time"

	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type SystemService struct {
	cfg         *config.Config
	settingsCol *mongodriver.Collection
	rdb         *redisdriver.Client
}

func NewSystemService(cfg *config.Config, db *mongodriver.Database, rdb *redisdriver.Client) *SystemService {
	return &SystemService{
		cfg:         cfg,
		settingsCol: db.Collection("system_settings"),
		rdb:         rdb,
	}
}

func (s *SystemService) GetSettings(ctx context.Context) (*domain.SystemSettings, error) {
	key := internalredis.SystemSettingsKey()

	// 1. Try cache
	val, err := internalredis.WithRetry(ctx, s.rdb, func(tCtx context.Context) (string, error) {
		return s.rdb.Get(tCtx, key).Result()
	})
	if err == nil {
		var settings domain.SystemSettings
		if err := json.Unmarshal([]byte(val), &settings); err == nil {
			return &settings, nil
		}
	}

	// 2. Try DB
	var settings domain.SystemSettings
	err = s.settingsCol.FindOne(ctx, bson.M{"slug": "global"}).Decode(&settings)
	if err != nil && err != mongodriver.ErrNoDocuments {
		return nil, err
	}

	if err == mongodriver.ErrNoDocuments {
		// Return defaults if nothing in DB yet
		settings = domain.SystemSettings{
			Slug:                                    "global",
			DefaultPlanID:                           "free",
			DefaultQueueJoinCodeLength:              6,
			DefaultQueueMaxJoinCodeAttempts:         5,
			DefaultQueueEntryIdleTimeoutMin:         30,
			DefaultQueueEntryGraceTimerSec:          120,
			DefaultQueueEntryRepositionOffset:       2,
			DefaultQueueSubscriptionGracePeriodDays: 3,
			SupportEmail:                            s.cfg.SupportEmail,
		}
	}

	// 3. Update cache (TTL 1 hour)
	data, _ := json.Marshal(settings)
	_ = internalredis.ExecRetry(ctx, s.rdb, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, data, 1*time.Hour).Err()
	})

	return &settings, nil
}

func (s *SystemService) UpdateSettings(ctx context.Context, settings *domain.SystemSettings) error {
	settings.Slug = "global"
	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.settingsCol.UpdateOne(ctx, bson.M{"slug": "global"}, bson.M{"$set": settings}, opts)
	if err != nil {
		return err
	}

	// Invalidate cache
	key := internalredis.SystemSettingsKey()
	_ = internalredis.ExecRetry(ctx, s.rdb, func(tCtx context.Context) error {
		return s.rdb.Del(tCtx, key).Err()
	})

	return nil
}
