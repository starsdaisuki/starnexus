package analytics

import (
	"log"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// AlertFunc is called when an anomaly or daily report needs to be sent.
type AlertFunc func(message string)

// Scheduler runs analytics jobs on schedule.
type Scheduler struct {
	db         *db.DB
	alertFn    AlertFunc
	mistralKey string
	stop       chan struct{}
}

func NewScheduler(database *db.DB, alertFn AlertFunc, mistralKey string) *Scheduler {
	return &Scheduler{
		db:         database,
		alertFn:    alertFn,
		mistralKey: mistralKey,
		stop:       make(chan struct{}),
	}
}

// Start launches all scheduled analytics goroutines.
func (s *Scheduler) Start() {
	go s.anomalyLoop()
	go s.downsampleLoop()
	go s.reportLoop()
	log.Println("[analytics] Scheduler started (anomaly: 5min, downsample: 03:00 UTC+8, report: 09:00 UTC+8)")
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

// GenerateReport exposes report generation for on-demand /report command.
func (s *Scheduler) GenerateReport() string {
	return GenerateDailyReport(s.db, s.mistralKey)
}

// anomalyLoop runs anomaly detection every 5 minutes.
func (s *Scheduler) anomalyLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			alerts := RunAnomalyDetection(s.db)
			for _, a := range alerts {
				msg := a.String()
				log.Printf("[analytics] Anomaly: %s", msg)
				if s.alertFn != nil {
					s.alertFn(msg)
				}
			}
		case <-s.stop:
			return
		}
	}
}

// downsampleLoop runs at 03:00 UTC+8 (19:00 UTC).
func (s *Scheduler) downsampleLoop() {
	for {
		waitUntilUTCHour(19, s.stop)
		select {
		case <-s.stop:
			return
		default:
		}
		log.Println("[analytics] Running downsampling...")
		RunDownsample(s.db)
		RunScoring(s.db)
	}
}

// reportLoop runs at 09:00 UTC+8 (01:00 UTC).
func (s *Scheduler) reportLoop() {
	for {
		waitUntilUTCHour(1, s.stop)
		select {
		case <-s.stop:
			return
		default:
		}
		log.Println("[analytics] Generating daily report...")
		msg := GenerateDailyReport(s.db, s.mistralKey)
		if msg != "" && s.alertFn != nil {
			s.alertFn(msg)
		}
	}
}

func waitUntilUTCHour(hour int, stop chan struct{}) {
	now := time.Now().UTC()
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if now.After(target) {
		target = target.Add(24 * time.Hour)
	}
	d := target.Sub(now)
	log.Printf("[analytics] Next job at %02d:00 UTC in %s", hour, d.Round(time.Minute))

	select {
	case <-time.After(d):
	case <-stop:
	}
}
