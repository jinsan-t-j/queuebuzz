package redis

import "fmt"

// Redis Key Patterns for consistency across modules
func UserSessionKey(queueID, entryID string) string {
	return fmt.Sprintf("user_session:%s:%s", queueID, entryID)
}

func QueuePositionsKey(queueID string) string {
	return fmt.Sprintf("queue_positions:%s", queueID)
}

func TicketCounterKey(queueID string) string {
	return fmt.Sprintf("ticket_counter:%s", queueID)
}

func IdleTimerKey(queueID, entryID string) string {
	return fmt.Sprintf("idle_timer:%s:%s", queueID, entryID)
}

func GraceTimerKey(queueID, entryID string) string {
	return fmt.Sprintf("grace_timer:%s:%s", queueID, entryID)
}

func ActionLockKey(queueID, action string) string {
	return fmt.Sprintf("lock:%s:%s", action, queueID)
}
func BillingPlansKey(country string) string {
	return fmt.Sprintf("billing_plans:%s", country)
}
func SystemSettingsKey() string {
	return "system_settings:global"
}
