package dto

import (
	"queuebuzz/internal/modules/queue/domain"
	"time"
)

type EntryResponse struct {
	ID          string  `json:"id"`
	QueueID     string  `json:"queue_id"`
	TicketNo    string  `json:"ticket_no"`
	Position    int64   `json:"position"`
	Name        string  `json:"name"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Status      string  `json:"status"`
	PartySize   *int    `json:"party_size"`
	WaitTimeMin int     `json:"wait_time_min"`
	ServedAt    *string `json:"served_at,omitempty"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func ToEntryResponse(entry domain.Entry, position int64) EntryResponse {
	var waitTime int
	if entry.ServedAt != nil {
		waitTime = int(entry.ServedAt.Sub(entry.CreatedAt).Minutes())
	} else if entry.FinishedAt != nil {
		waitTime = int(entry.FinishedAt.Sub(entry.CreatedAt).Minutes())
	}

	return EntryResponse{
		ID:          entry.ID,
		QueueID:     entry.QueueID,
		TicketNo:    entry.TicketNo,
		Position:    position,
		Name:        entry.Name,
		Email:       entry.Email,
		Phone:       entry.Phone,
		Status:      entry.Status,
		PartySize:   entry.PartySize,
		WaitTimeMin: waitTime,
		ServedAt:    formatOptionalTime(entry.ServedAt),
		FinishedAt:  formatOptionalTime(entry.FinishedAt),
		CreatedAt:   entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   entry.UpdatedAt.Format(time.RFC3339),
	}
}

type StatusResponse struct {
	Entry EntryResponse `json:"entry"`
}
