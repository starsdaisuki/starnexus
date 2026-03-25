package analytics

import (
	"log"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// RunDownsample aggregates raw → hourly (7-30 days), hourly → daily (30+ days),
// and purges old raw data (>7 days) and old hourly data (>30 days).
func RunDownsample(database *db.DB) {
	now := time.Now().Unix()
	sevenDaysAgo := now - 7*86400
	thirtyDaysAgo := now - 30*86400

	// Aggregate raw metrics older than 7 days into hourly
	log.Println("[analytics] Aggregating raw → hourly (7-30 days)...")
	if err := database.AggregateHourly(thirtyDaysAgo, sevenDaysAgo); err != nil {
		log.Printf("[analytics] Hourly aggregation error: %v", err)
	}

	// Aggregate hourly metrics older than 30 days into daily
	log.Println("[analytics] Aggregating hourly → daily (30+ days)...")
	if err := database.AggregateDaily(0, thirtyDaysAgo); err != nil {
		log.Printf("[analytics] Daily aggregation error: %v", err)
	}

	// Purge raw metrics older than 7 days
	purged, err := database.PurgeRawMetrics(sevenDaysAgo)
	if err != nil {
		log.Printf("[analytics] Raw purge error: %v", err)
	} else if purged > 0 {
		log.Printf("[analytics] Purged %d raw metrics (>7 days)", purged)
	}

	// Purge hourly metrics older than 30 days (already aggregated to daily)
	purged, err = database.PurgeHourlyMetrics(thirtyDaysAgo)
	if err != nil {
		log.Printf("[analytics] Hourly purge error: %v", err)
	} else if purged > 0 {
		log.Printf("[analytics] Purged %d hourly metrics (>30 days)", purged)
	}

	log.Println("[analytics] Downsampling complete")
}
