package service

import (
	"context"
	"encoding/json"
	"time"

	"queuebuzz/internal/modules/billing/domain"
)

const hostPlanCacheKeyPrefix = "billing:host_plan:"

func (s *BillingService) getCachedHostPlan(ctx context.Context, hostID string) *domain.Plan {
	if s.redis == nil {
		return nil
	}

	cacheKey := hostPlanCacheKeyPrefix + hostID
	val, err := s.redis.Get(ctx, cacheKey).Result()
	if err != nil {
		return nil
	}

	var plan domain.Plan
	if err := json.Unmarshal([]byte(val), &plan); err != nil {
		return nil
	}

	return &plan
}

func (s *BillingService) setCacheHostPlan(ctx context.Context, hostID string, plan *domain.Plan, ttl time.Duration) {
	if s.redis == nil || hostID == "" || plan == nil {
		return
	}

	cacheKey := hostPlanCacheKeyPrefix + hostID
	data, _ := json.Marshal(plan)
	_ = s.redis.Set(ctx, cacheKey, data, ttl).Err()
}

func (s *BillingService) invalidateHostPlanCache(ctx context.Context, hostID string) {
	if s.redis == nil || hostID == "" {
		return
	}

	cacheKey := hostPlanCacheKeyPrefix + hostID
	_ = s.redis.Del(ctx, cacheKey).Err()
}
