package dto

type DashboardData struct {
	ActiveQueue    *ActiveQueueStats `json:"activeQueue"`
	Stats          DashboardStats    `json:"stats"`
	WeekChart      []ChartDataPoint  `json:"weekChart"`
	RecentSessions []RecentSession   `json:"recentSessions"`
	ReturnRate     ReturnRateStats   `json:"returnRate"`
	DroppedSkipped []HeatmapPoint    `json:"droppedSkipped"`
	PeakHours      []PeakHourPoint   `json:"peakHours"`
	Greeting       GreetingData      `json:"greeting"`
	QuickSetup     QuickSetupData    `json:"quickSetup"`
	HasHistory     bool              `json:"hasHistory"`
}

type ActiveQueueStats struct {
	IsActive  bool   `json:"isActive"`
	QueueName string `json:"queueName"`
	StartedAt string `json:"startedAt"`
}

type DashboardStats struct {
	ServedToday int    `json:"servedToday"`
	AvgWait     string `json:"avgWait"`
	PeakWait    int    `json:"peakWait"`
	Skipped     int    `json:"skipped"`
}

type ChartDataPoint struct {
	Day      string `json:"day"`
	Value    int    `json:"value"`
	AvgWait  int    `json:"avgWait"` // in seconds
	IsFuture bool   `json:"isFuture"`
	IsToday  bool   `json:"isToday"`
}

type RecentSession struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Duration string `json:"duration"`
	Served   int    `json:"served"`
}

type ReturnRateStats struct {
	HasData        bool              `json:"hasData"`
	ChartData      []ReturnRatePoint `json:"chartData"`
	ChartDataToday []ReturnRatePoint `json:"chartDataToday"`
	ByQueue        []ReturnRateQueue `json:"byQueue"`
	ReturningCount int               `json:"returningCount"`
}

type ReturnRatePoint struct {
	Day  string  `json:"day"`
	Rate float64 `json:"rate"`
}

type ReturnRateQueue struct {
	Label string  `json:"label"`
	Rate  float64 `json:"rate"`
}

type HeatmapPoint struct {
	Hour  int `json:"hour"`
	Day   int `json:"day"`
	Value int `json:"value"`
}

type PeakHourPoint struct {
	Hour  string `json:"hour"`
	Day   int    `json:"day"`
	Value int    `json:"value"`
}

type GreetingData struct {
	Name string `json:"name"`
}

type QuickSetupData struct {
	Show  bool             `json:"show"`
	Steps []QuickSetupStep `json:"steps"`
}

type QuickSetupStep struct {
	Label  string `json:"label"`
	Sub    string `json:"sub"`
	IsDone bool   `json:"isDone"`
}
