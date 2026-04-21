package alert

import (
	"log"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type Monitor struct {
	db        *db.DB
	threshold int // seconds
	stop      chan struct{}
}

func NewMonitor(database *db.DB, thresholdSeconds int) *Monitor {
	return &Monitor{
		db:        database,
		threshold: thresholdSeconds,
		stop:      make(chan struct{}),
	}
}

// Start runs the offline detection loop every 30 seconds.
func (m *Monitor) Start() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.checkOffline()
			case <-m.stop:
				return
			}
		}
	}()
	log.Printf("Offline monitor started (threshold: %ds)", m.threshold)
}

func (m *Monitor) Stop() {
	close(m.stop)
}

func (m *Monitor) checkOffline() {
	stale, err := m.db.GetStaleOnlineNodes(m.threshold)
	if err != nil {
		log.Printf("Offline check error: %v", err)
		return
	}

	for _, node := range stale {
		if err := m.db.SetNodeOffline(node.ID); err != nil {
			log.Printf("Failed to set node %s offline: %v", node.ID, err)
			continue
		}
		if err := m.db.RecordStatusChange(node.ID, node.OldStatus, "offline", "No report received within threshold"); err != nil {
			log.Printf("Failed to record status change for %s: %v", node.ID, err)
		}
		_ = m.db.RecordEvent(node.ID, "status_change", "critical", "Node offline", "No report received within offline threshold", "")
		log.Printf("Node %s marked offline (was %s)", node.ID, node.OldStatus)
	}
}
