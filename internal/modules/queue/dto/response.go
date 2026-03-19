package dto

type QueueRecord struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	JoinCode       string `json:"join_code"`
	Slug           string `json:"slug"`
	Status         string `json:"status"`
	AvgServiceMins int    `json:"avg_service_mins"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
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
