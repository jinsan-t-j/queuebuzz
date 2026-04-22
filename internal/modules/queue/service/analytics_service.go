package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type AnalyticsService struct {
	queueCol *mongodriver.Collection
	entryCol *mongodriver.Collection
}

func NewAnalyticsService(queueCol *mongodriver.Collection, entryCol *mongodriver.Collection) *AnalyticsService {
	return &AnalyticsService{
		queueCol: queueCol,
		entryCol: entryCol,
	}
}

// ProcessIdentity calculates the IdentityHash and determines if a customer is returning.
func (s *AnalyticsService) ProcessIdentity(ctx context.Context, entry *domain.Entry, queue *domain.Queue) {
	hostIdentifier := ""
	if queue.HostPublicID != nil {
		hostIdentifier = *queue.HostPublicID
	} else if queue.HostID != nil {
		hostIdentifier = *queue.HostID
	}

	rawIdentity := ""
	if entry.Phone != nil && *entry.Phone != "" {
		rawIdentity = "phone:" + strings.TrimSpace(*entry.Phone)
	} else if entry.Email != nil && *entry.Email != "" {
		rawIdentity = "email:" + strings.ToLower(strings.TrimSpace(*entry.Email))
	} else if entry.Fingerprint != "" {
		rawIdentity = "fp:" + entry.Fingerprint
	}

	if rawIdentity != "" && hostIdentifier != "" {
		hash := sha256.New()
		hash.Write([]byte(hostIdentifier + "|" + rawIdentity))
		entry.IdentityHash = hex.EncodeToString(hash.Sum(nil))

		// Check for returning status
		count, _ := s.entryCol.CountDocuments(ctx, bson.M{
			"identity_hash": entry.IdentityHash,
			"queue_id":      bson.M{"$ne": entry.QueueID},
		})
		entry.IsReturning = count > 0
	}
}

func (s *AnalyticsService) GetDashboardData(ctx context.Context, hostPublicID string, hostName string) (*dto.DashboardData, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfToday.AddDate(0, 0, -6)

	// 1. Get Active Queue
	var activeQ domain.Queue
	activeFound := true
	if err := s.queueCol.FindOne(ctx, bson.M{
		"host_public_id": hostPublicID,
		"status":         bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
	}).Decode(&activeQ); err != nil {
		activeFound = false
	}

	// 2. Aggregate Data (using 7-day window)
	cursor, err := s.queueCol.Find(ctx, bson.M{
		"host_public_id": hostPublicID,
		"created_at":     bson.M{"$gte": startOfWeek},
	})
	if err != nil {
		return nil, err
	}
	var queues []domain.Queue
	if err := cursor.All(ctx, &queues); err != nil {
		return nil, err
	}

	queueIDs := make([]string, len(queues))
	for i, q := range queues {
		queueIDs[i] = q.ID
	}

	entryCursor, err := s.entryCol.Find(ctx, bson.M{
		"queue_id": bson.M{"$in": queueIDs},
	})
	if err != nil {
		return nil, err
	}
	var entries []domain.Entry
	if err := entryCursor.All(ctx, &entries); err != nil {
		return nil, err
	}

	// 3. Process Aggregations
	stats := dto.DashboardStats{}
	var totalWaitToday time.Duration
	peakWaitToday := 0

	weekMap := make(map[string]int)
	peakHoursMap := make(map[int]int)
	returnByDayMap := make(map[string]struct{ total, returning int })
	droppedSkippedMap := make(map[string]int) // "hour,day" -> value

	for _, e := range entries {
		eDate := e.CreatedAt.In(now.Location())
		eDay := eDate.Format("MON")

		// CATEGORIZATION SWITCH
		switch e.Status {
		case constants.EntryStatusServed:
			// 1. Week Chart (Historical)
			weekMap[eDay]++

			// 2. Stats Today
			if eDate.After(startOfToday) {
				stats.ServedToday++
				if e.ServedAt != nil {
					wait := e.ServedAt.Sub(e.CreatedAt)
					totalWaitToday += wait
					waitMins := int(wait.Minutes())
					if waitMins > peakWaitToday {
						peakWaitToday = waitMins
					}
				}
			}

		case constants.EntryStatusSkipped, constants.EntryStatusLeft:
			// 1. Stats Today
			if eDate.After(startOfToday) {
				stats.Skipped++

				// Heatmap for dropped/skipped
				dayIdx := int(eDate.Weekday())
				key := fmt.Sprintf("%d,%d", eDate.Hour(), dayIdx)
				droppedSkippedMap[key]++
			}
		}

		// COMMON METRICS (regardless of status)

		// Return Rate Tracking
		row := returnByDayMap[eDay]
		row.total++
		if e.IsReturning {
			row.returning++
		}
		returnByDayMap[eDay] = row

		// Peak Hours tracking
		peakHoursMap[eDate.Hour()]++
	}

	// Formatting Stats
	stats.PeakWait = peakWaitToday
	if stats.ServedToday > 0 {
		avg := totalWaitToday / time.Duration(stats.ServedToday)
		stats.AvgWait = fmt.Sprintf("%dm %ds", int(avg.Minutes()), int(avg.Seconds())%60)
	} else {
		stats.AvgWait = "0m"
	}

	// Building Week Chart
	weekChart := make([]dto.ChartDataPoint, 7)
	for i := 0; i < 7; i++ {
		date := startOfWeek.AddDate(0, 0, i)
		day := date.Format("MON")
		weekChart[i] = dto.ChartDataPoint{
			Day:      day,
			Value:    weekMap[day],
			IsToday:  date.Format("2006-01-02") == now.Format("2006-01-02"),
			IsFuture: false,
		}
	}

	// Building Peak Hours
	peakHoursItems := make([]dto.PeakHourPoint, 24)
	for h := 0; h < 24; h++ {
		label := fmt.Sprintf("%d AM", h)
		if h == 0 {
			label = "12 AM"
		} else if h == 12 {
			label = "12 PM"
		} else if h > 12 {
			label = fmt.Sprintf("%d PM", h-12)
		}
		peakHoursItems[h] = dto.PeakHourPoint{
			Hour:  label,
			Value: peakHoursMap[h],
		}
	}

	// Recent Sessions (last 3 closed queues)
	recentSessions := []dto.RecentSession{}
	closedCount := 0
	for i := len(queues) - 1; i >= 0 && closedCount < 3; i-- {
		q := queues[i]
		if q.Status == constants.QueueStatusClosed || q.Status == constants.QueueStatusExpired {
			duration := "—"
			if q.ClosedAt != nil {
				d := q.ClosedAt.Sub(q.CreatedAt)
				duration = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
			}

			// Count served for THIS queue
			qServed := 0
			for _, e := range entries {
				if e.QueueID == q.ID && e.Status == constants.EntryStatusServed {
					qServed++
				}
			}

			recentSessions = append(recentSessions, dto.RecentSession{
				ID:       q.ID,
				Name:     q.Name,
				Date:     q.CreatedAt.Format("02 Jan"),
				Duration: duration,
				Served:   qServed,
			})
			closedCount++
		}
	}

	// Return Rate
	returnChart := make([]dto.ReturnRatePoint, 7)
	totalReturning := 0
	for i := 0; i < 7; i++ {
		day := weekChart[i].Day
		row := returnByDayMap[day]
		rate := 0.0
		if row.total > 0 {
			rate = (float64(row.returning) / float64(row.total)) * 100
		}
		returnChart[i] = dto.ReturnRatePoint{Day: day, Rate: rate}
		totalReturning += row.returning
	}

	// Dropped/Skipped Heatmap
	heatmap := []dto.HeatmapPoint{}
	for key, val := range droppedSkippedMap {
		var h, d int
		fmt.Sscanf(key, "%d,%d", &h, &d)
		heatmap = append(heatmap, dto.HeatmapPoint{Hour: h, Day: d, Value: val})
	}

	return &dto.DashboardData{
		ActiveQueue: &dto.ActiveQueueStats{
			IsActive:  activeFound,
			QueueName: activeQ.Name,
			StartedAt: activeQ.CreatedAt.Format("3:04 PM"),
		},
		Stats:          stats,
		WeekChart:      weekChart,
		RecentSessions: recentSessions,
		ReturnRate: dto.ReturnRateStats{
			HasData:        len(entries) > 0,
			ChartData:      returnChart,
			ReturningCount: totalReturning,
		},
		DroppedSkipped: heatmap,
		PeakHours:      peakHoursItems,
		Greeting:       dto.GreetingData{Name: hostName},
		QuickSetup: dto.QuickSetupData{
			Show: !activeFound && len(queues) < 5,
			Steps: []dto.QuickSetupStep{
				{Label: "Create your first queue", Sub: "Define your service parameters", IsDone: len(queues) > 0},
				{Label: "Connect your first customer", Sub: "Start your journey", IsDone: stats.ServedToday > 0},
				{Label: "Complete your first queue", Sub: "Grab your streak", IsDone: closedCount > 0},
			},
		},
	}, nil
}
