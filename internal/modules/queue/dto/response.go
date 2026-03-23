package dto

import (
	"queuebuzz/internal/modules/queue/domain"
	"time"
)

type QueueRecord struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	JoinCode          string  `json:"join_code"`
	Slug              string  `json:"slug"`
	Status            string  `json:"status"`
	AvgServiceMins    int     `json:"avg_service_mins"`
	AllowPartyJoining bool    `json:"allow_party_joining"`
	MaxPartySize      int     `json:"max_party_size"`
	RecoveryEmail     *string `json:"recovery_email,omitempty"`
	CreatedAt         string  `json:"created_at"`
	ExpiresAt         string  `json:"expires_at"`
}

type EntryRecord struct {
	ID               string  `json:"id"`
	TicketNo         string  `json:"ticket_no"`
	Position         int     `json:"position"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	EstimatedWaitMin int64   `json:"estimated_wait_min"`
	PartySize        *int    `json:"party_size"`
	JoinedAt         string  `json:"joined_at"`
	ServedAt         string  `json:"served_at,omitempty"`
	FinishedAt       string  `json:"finished_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	CreatedBy        *string `json:"created_by,omitempty"`
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

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}

	return value.Format(time.RFC3339)
}

func ToEntryResponse(entry domain.Entry, position int) EntryRecord {
	return EntryRecord{
		ID:         entry.ID,
		TicketNo:   entry.TicketNo,
		Position:   position,
		Status:     entry.Status,
		Name:       entry.Name,
		PartySize:  entry.PartySize,
		CreatedBy:  entry.CreatedBy,
		JoinedAt:   entry.JoinedAt.Format(time.RFC3339),
		ServedAt:   formatOptionalTime(entry.ServedAt),
		FinishedAt: formatOptionalTime(entry.FinishedAt),
		CreatedAt:  entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  entry.UpdatedAt.Format(time.RFC3339),
	}
}

func ToEntryResponses(entries []domain.Entry) []EntryRecord {
	responses := make([]EntryRecord, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, ToEntryResponse(entry, entry.Position))
	}
	return responses
}
