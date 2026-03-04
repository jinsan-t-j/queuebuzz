package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ticketInstance *TicketService
	ticketOnce     sync.Once
)

type TicketService struct {
	rdb *redis.Client
}

func NewTicketService(rdb *redis.Client) *TicketService {
	ticketOnce.Do(func() {
		ticketInstance = &TicketService{rdb: rdb}
	})

	return ticketInstance
}

// NextTicket atomically increments the ticket counter for the given queue
// and returns a formatted ticket number like "Q-0042".
func (s *TicketService) NextTicket(ctx context.Context, queueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("ticket_counter:%s", queueID)
	num, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("failed to increment ticket counter: %w", err)
	}

	return fmt.Sprintf("Q-%04d", num), nil
}
