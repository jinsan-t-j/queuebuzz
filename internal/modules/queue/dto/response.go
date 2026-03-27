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

type QueueStatus struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	JoinCode       string `json:"join_code"`
	AvgServiceMins int    `json:"avg_service_mins"`
	ExpiresAt      string `json:"expires_at"`
}

type EntryRecord struct {
	ID         string  `json:"id"`
	TicketNo   string  `json:"ticket_no"`
	Position   int64   `json:"position"`
	Name       string  `json:"name"`
	Email      *string `json:"email,omitempty"`
	Phone      *string `json:"phone,omitempty"`
	Status     string  `json:"status"`
	PartySize  *int    `json:"party_size"`
	ServedAt   string  `json:"served_at,omitempty"`
	FinishedAt string  `json:"finished_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	CreatedBy  *string `json:"created_by,omitempty"`
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

type GetStatusResponse struct {
	Queue        QueueStatus `json:"queue"`
	WaitingCount int64       `json:"waiting_count"`
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

func ToEntryResponse(entry domain.Entry, position int64) EntryRecord {
	return EntryRecord{
		ID:         entry.ID,
		TicketNo:   entry.TicketNo,
		Position:   position,
		Status:     entry.Status,
		Name:       entry.Name,
		Email:      entry.Email,
		Phone:      entry.Phone,
		PartySize:  entry.PartySize,
		CreatedBy:  entry.CreatedBy,
		ServedAt:   formatOptionalTime(entry.ServedAt),
		FinishedAt: formatOptionalTime(entry.FinishedAt),
		CreatedAt:  entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  entry.UpdatedAt.Format(time.RFC3339),
	}
}

func ToEntryResponses(entries []domain.Entry) []EntryRecord {
	records := make([]EntryRecord, len(entries))
	for i, e := range entries {
		records[i] = ToEntryResponse(e, int64(i+1))
	}
	return records
}

func ToQueueResponse(queue domain.Queue) QueueRecord {
	return QueueRecord{
		ID:                queue.ID,
		Name:              queue.Name,
		JoinCode:          queue.JoinCode,
		Slug:              queue.Slug,
		Status:            queue.Status,
		AvgServiceMins:    queue.AvgServiceMins,
		AllowPartyJoining: queue.AllowPartyJoining,
		MaxPartySize:      queue.MaxPartySize,
		RecoveryEmail:     queue.RecoveryEmail,
		CreatedAt:         queue.CreatedAt.Format(time.RFC3339),
		ExpiresAt:         queue.ExpiresAt.Format(time.RFC3339),
	}
}
