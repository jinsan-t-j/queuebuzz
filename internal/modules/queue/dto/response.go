package dto

import (
	"queuebuzz/internal/modules/queue/domain"
	"time"
)

type QueueRecord struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	JoinCode            string  `json:"join_code"`
	Slug                string  `json:"slug"`
	Status              string  `json:"status"`
	AvgServiceMins      int     `json:"avg_service_mins"`
	AllowPartyJoining   bool    `json:"allow_party_joining"`
	MaxPartySize        int     `json:"max_party_size"`
	ManualPositioning   bool    `json:"manual_positioning"`
	StrictQueueMode     bool    `json:"strict_queue_mode"`
	CollectEmails       bool    `json:"collect_emails"`
	Notes               string  `json:"notes,omitempty"`
	HostProfileImageURL string  `json:"host_profile_image_url,omitempty"`
	HostBannerImageURL  string  `json:"host_banner_image_url,omitempty"`
	IsGeoLocked         bool    `json:"is_geo_locked"`
	Latitude            float64 `json:"latitude"`
	Longitude           float64 `json:"longitude"`
	GeoRadiusMeters     float64 `json:"geo_radius_meters"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	ExpiresAt           string  `json:"expires_at"`
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
	VerifyCode  string  `json:"verify_code"`
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

type HistoryDetailResponse struct {
	QueueName string          `json:"queue_name"`
	Date      string          `json:"date"`
	Status    string          `json:"status"`
	Notes     string          `json:"notes"`
	ClosedAt  *string         `json:"closed_at,omitempty"`
	Stats     SessionStats    `json:"stats"`
	Insights  []string        `json:"insights"`
	Timeline  []TimelineEvent `json:"timeline"`
	Entries   []HistoryEntry  `json:"entries"`
}

type SessionStats struct {
	TotalBookings int    `json:"total_bookings"`
	TotalServed   int    `json:"total_served"`
	TotalSkipped  int    `json:"total_skipped"`
	AvgWaitTime   string `json:"avg_wait_time"`
	PeakVolume    string `json:"peak_volume"`
}

type TimelineEvent struct {
	Type      string             `json:"type"`
	Timestamp string             `json:"timestamp"`
	Message   string             `json:"message"`
	Color     string             `json:"color"`
	SubEvents []TimelineSubEvent `json:"sub_events,omitempty"`
}

type TimelineSubEvent struct {
	TicketNo string `json:"ticket_no"`
	Name     string `json:"name"`
	Action   string `json:"action"`
	Time     string `json:"time"`
}

type HistoryEntry struct {
	TicketNo    string  `json:"ticket_no"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Status      string  `json:"status"`
	WaitTimeMin int     `json:"wait_time_min"`
	ServedAt    string  `json:"served_at,omitempty"`
}

type HistoryListResponse struct {
	Data       []QueueHistoryListItem `json:"data"`
	TotalCount int                    `json:"total_count"`
	TotalPages int                    `json:"total_pages"`
	Summary    HistorySummary         `json:"summary"`
}

type HistorySummary struct {
	TotalSessions    int    `json:"total_sessions"`
	TotalServed      int    `json:"total_served"`
	AvgSessionLength string `json:"avg_session_length"`
}

type QueueHistoryListItem struct {
	ID            string `json:"id"`
	Date          string `json:"date"`
	DateFormatted string `json:"date_formatted"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	TotalServed   int    `json:"total_served"`
	AvgWait       string `json:"avg_wait"`
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
		VerifyCode:  entry.VerifyCode,
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

func ToQueueResponse(queue domain.Queue, profileImg, bannerImg string) QueueRecord {
	return QueueRecord{
		ID:                  queue.ID,
		Name:                queue.Name,
		JoinCode:            queue.JoinCode,
		Slug:                queue.Slug,
		Status:              queue.Status,
		AvgServiceMins:      queue.AvgServiceMins,
		AllowPartyJoining:   queue.AllowPartyJoining,
		MaxPartySize:        queue.MaxPartySize,
		ManualPositioning:   queue.ManualPositioning,
		StrictQueueMode:     queue.StrictQueueMode,
		CollectEmails:       queue.CollectEmails,
		Notes:               queue.Notes,
		HostProfileImageURL: profileImg,
		HostBannerImageURL:  bannerImg,
		IsGeoLocked:         queue.IsGeoLocked,
		Latitude:            queue.Latitude,
		Longitude:           queue.Longitude,
		GeoRadiusMeters:     queue.GeoRadiusMeters,
		CreatedAt:           queue.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           queue.UpdatedAt.Format(time.RFC3339),
		ExpiresAt:           queue.ExpiresAt.Format(time.RFC3339),
	}
}
