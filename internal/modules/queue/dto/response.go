package dto

type QueueRecord struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	JoinCode          string `json:"join_code"`
	Slug              string `json:"slug"`
	Status            string `json:"status"`
	AvgServiceMins    int    `json:"avg_service_mins"`
	AllowPartyJoining bool   `json:"allow_party_joining"`
	MaxPartySize      int    `json:"max_party_size"`
	CreatedAt         string `json:"created_at"`
	ExpiresAt         string `json:"expires_at"`
}

type EntryRecord struct {
	ID               string `json:"id"`
	TicketNo         string `json:"ticket_no"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	EstimatedWaitMin int64  `json:"estimated_wait_min"`
	PartySize        *int   `json:"party_size"`
	ServedAt         string `json:"served_at,omitempty"`
}

type CreateQueueResponse struct {
	QueueRecord
}

type GetLiveQueueResponse struct {
	QueueRecord
}

type CheckSlugResponse struct {
	IsAvailable bool `json:"is_available"`
}

type AddEntryResponse struct {
	EntryRecord
}

type QueueHistoryResponse struct {
	Entries []HistoryEntry `json:"entries"`
}

type HistoryEntry struct {
	TicketNo    string `json:"ticket_no"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	WaitTimeMin int    `json:"wait_time_min"`
	ServedAt    string `json:"served_at,omitempty"`
}
