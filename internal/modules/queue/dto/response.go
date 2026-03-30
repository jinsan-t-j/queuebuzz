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
	ID          string  `json:"id"`
	QueueID     string  `json:"queue_id"`
	TicketNo    string  `json:"ticket_no"`
	Position    int64   `json:"position"`
	Name        string  `json:"name"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Status      string  `json:"status"`
	PartySize   *int    `json:"party_size"`
	ServedAt    *string `json:"served_at,omitempty"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	WaitTimeMin int     `json:"wait_time_min"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CreatedBy   *string `json:"created_by,omitempty"`
}

type CheckSlugResponse struct {
	IsAvailable bool `json:"is_available"`
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

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.Format(time.RFC3339)
	return &formatted
}

func ToEntryResponse(entry domain.Entry, position int64) EntryRecord {
	var waitTime int
	if entry.ServedAt != nil {
		waitTime = int(entry.ServedAt.Sub(entry.CreatedAt).Minutes())
	} else if entry.FinishedAt != nil {
		waitTime = int(entry.FinishedAt.Sub(entry.CreatedAt).Minutes())
	}

	return EntryRecord{
		ID:          entry.ID,
		QueueID:     entry.QueueID,
		TicketNo:    entry.TicketNo,
		Position:    position,
		Name:        entry.Name,
		Email:       entry.Email,
		Phone:       entry.Phone,
		Status:      entry.Status,
		PartySize:   entry.PartySize,
		ServedAt:    formatOptionalTime(entry.ServedAt),
		FinishedAt:  formatOptionalTime(entry.FinishedAt),
		WaitTimeMin: waitTime,
		CreatedAt:   entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   entry.UpdatedAt.Format(time.RFC3339),
		CreatedBy:   entry.CreatedBy,
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
