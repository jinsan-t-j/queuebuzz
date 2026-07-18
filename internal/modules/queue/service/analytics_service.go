package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type AnalyticsService struct {
	queueCol *mongodriver.Collection
	entryCol *mongodriver.Collection
}

type dashboardCacheEntry struct {
	expiresAt time.Time
	payload   []byte
}

var dashboardCache sync.Map

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

	if cached, ok := loadDashboardCache(hostPublicID); ok {
		var data dto.DashboardData
		if err := json.Unmarshal(cached, &data); err == nil {
			return &data, nil
		}
	}

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
	weekWaitMap := make(map[string]time.Duration)
	peakHoursMap := make(map[string]int)
	returnByDayMap := make(map[string]struct{ total, returning int })
	returnByHourMap := make(map[int]struct{ total, returning int })
	droppedSkippedMap := make(map[string]int) // "hour,day" -> value

	for _, e := range entries {
		eDate := e.CreatedAt.In(now.Location())
		eDay := eDate.Format("Mon")

		// CATEGORIZATION SWITCH
		switch e.Status {
		case constants.EntryStatusServed:
			// 1. Week Chart (Historical)
			weekMap[eDay]++
			if e.ServedAt != nil {
				weekWaitMap[eDay] += e.ServedAt.Sub(e.CreatedAt)
			}

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
			}

			// Heatmap for dropped/skipped
			dayIdx := int(eDate.Weekday())
			key := fmt.Sprintf("%d,%d", eDate.Hour(), dayIdx)
			droppedSkippedMap[key]++
		}

		// COMMON METRICS (regardless of status)

		// Return Rate Tracking
		row := returnByDayMap[eDay]
		row.total++
		if e.IsReturning {
			row.returning++
		}
		returnByDayMap[eDay] = row

		// Today's hourly return rate tracking
		if eDate.After(startOfToday) {
			h := eDate.Hour()
			rowToday := returnByHourMap[h]
			rowToday.total++
			if e.IsReturning {
				rowToday.returning++
			}
			returnByHourMap[h] = rowToday
		}

		// Peak Hours tracking
		dayIdx := int(eDate.Weekday())
		peakHoursKey := fmt.Sprintf("%d,%d", eDate.Hour(), dayIdx)
		peakHoursMap[peakHoursKey]++
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
		day := date.Format("Mon")
		avgWait := 0
		if weekMap[day] > 0 {
			avgWait = int(weekWaitMap[day].Seconds() / float64(weekMap[day]))
		}
		weekChart[i] = dto.ChartDataPoint{
			Day:      day,
			Value:    weekMap[day],
			AvgWait:  avgWait,
			IsToday:  date.Format("2006-01-02") == now.Format("2006-01-02"),
			IsFuture: false,
		}
	}

	// Building Peak Hours
	peakHoursItems := []dto.PeakHourPoint{}
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			label := fmt.Sprintf("%d AM", h)
			if h == 0 {
				label = "12 AM"
			} else if h == 12 {
				label = "12 PM"
			} else if h > 12 {
				label = fmt.Sprintf("%d PM", h-12)
			}
			key := fmt.Sprintf("%d,%d", h, d)
			peakHoursItems = append(peakHoursItems, dto.PeakHourPoint{
				Hour:  label,
				Day:   d,
				Value: peakHoursMap[key],
			})
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

	returnChartToday := make([]dto.ReturnRatePoint, 24)
	for h := 0; h < 24; h++ {
		label := fmt.Sprintf("%d AM", h)
		if h == 0 {
			label = "12 AM"
		} else if h == 12 {
			label = "12 PM"
		} else if h > 12 {
			label = fmt.Sprintf("%d PM", h-12)
		}
		row := returnByHourMap[h]
		rate := 0.0
		if row.total > 0 {
			rate = (float64(row.returning) / float64(row.total)) * 100
		}
		returnChartToday[h] = dto.ReturnRatePoint{Day: label, Rate: rate}
	}

	// Dropped/Skipped Heatmap
	heatmap := []dto.HeatmapPoint{}
	for key, val := range droppedSkippedMap {
		var h, d int
		fmt.Sscanf(key, "%d,%d", &h, &d)
		heatmap = append(heatmap, dto.HeatmapPoint{Hour: h, Day: d, Value: val})
	}

	data := &dto.DashboardData{
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
			ChartDataToday: returnChartToday,
			ReturningCount: totalReturning,
		},
		DroppedSkipped: heatmap,
		PeakHours:      peakHoursItems,
		Greeting:       dto.GreetingData{Name: hostName},
		QuickSetup: dto.QuickSetupData{
			Show: len(queues) < 5,
			Steps: []dto.QuickSetupStep{
				{Label: "Create your first queue", Sub: "Define your service parameters", IsDone: len(queues) > 0},
				{Label: "Connect your first customer", Sub: "Start your journey", IsDone: stats.ServedToday > 0},
				{Label: "Complete your first queue", Sub: "Grab your streak", IsDone: closedCount > 0},
			},
		},
	}

	if payload, err := json.Marshal(data); err == nil {
		storeDashboardCache(hostPublicID, payload)
	}

	return data, nil
}

func loadDashboardCache(hostPublicID string) ([]byte, bool) {
	if hostPublicID == "" {
		return nil, false
	}

	entry, ok := dashboardCache.Load(hostPublicID)
	if !ok {
		return nil, false
	}

	cached, ok := entry.(dashboardCacheEntry)
	if !ok {
		dashboardCache.Delete(hostPublicID)
		return nil, false
	}

	if time.Now().After(cached.expiresAt) {
		dashboardCache.Delete(hostPublicID)
		return nil, false
	}

	return cached.payload, true
}

func storeDashboardCache(hostPublicID string, payload []byte) {
	if hostPublicID == "" || len(payload) == 0 {
		return
	}

	dashboardCache.Store(hostPublicID, dashboardCacheEntry{
		expiresAt: time.Now().Add(15 * time.Second),
		payload:   payload,
	})
}
