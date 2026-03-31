package constants

// Queue statuses
const (
	QueueStatusActive  = "ACTIVE"
	QueueStatusPaused  = "PAUSED"
	QueueStatusClosed  = "CLOSED"
	QueueStatusExpired = "EXPIRED"
)

// Queue entry statuses
const (
	EntryStatusWaiting = "WAITING"
	EntryStatusCalled  = "CALLED"
	EntryStatusIdle    = "IDLE"
	EntryStatusSkipped = "SKIPPED"
	EntryStatusServed  = "SERVED"
	EntryStatusLeft    = "LEFT"
	EntryStatusArrived = "ARRIVED"
)

// Host roles (JWT claim values)
const (
	RoleAnonymousHost  = "anonymous_host"
	RoleRegisteredHost = "registered_host"
)

// Host tiers
const (
	TierFree    = "FREE"
	TierPremium = "PREMIUM"
)

// Default values
const (
	DefaultAvgServiceMins = 5
	DefaultQueueExpiryH   = 24
	DefaultIdleTimeoutMin = 3
	DefaultGraceTimerSec  = 300
	JoinCodeLength        = 6
	JoinCodeCharset       = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	JoinCodeTTLExtraH     = 1 // 1h buffer beyond queue 24h expiry
	MaxJoinCodeAttempts   = 5
	HeartbeatTTLSec       = 90
)
