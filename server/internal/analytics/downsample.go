package analytics

import (
	"log"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// RunDownsample aggregates raw → hourly (7-30 days), hourly → daily (30+ days),
// and purges old raw data (>7 days) and old hourly data (>30 days).
//
// Both cutoffs are floored to their bucket boundary (hour / day). With a
// mid-bucket cutoff the nightly INSERT OR REPLACE would aggregate a
// partial bucket, and the next night — once the rest of the bucket ages
// past the cutoff — overwrite the earlier aggregate with only the
// remainder, permanently corrupting the archive.
func RunDownsample(database *db.DB) {
	now := time.Now().Unix()
	sevenDaysAgo := ((now - 7*86400) / 3600) * 3600
	thirtyDaysAgo := ((now - 30*86400) / 86400) * 86400

	// Aggregate raw metrics older than 7 days into hourly
	log.Println("[analytics] Aggregating raw → hourly (7-30 days)...")
	hourlyErr := database.AggregateHourly(thirtyDaysAgo, sevenDaysAgo)
	if hourlyErr != nil {
		log.Printf("[analytics] Hourly aggregation error: %v", hourlyErr)
	}

	// Aggregate hourly metrics older than 30 days into daily
	log.Println("[analytics] Aggregating hourly → daily (30+ days)...")
	dailyErr := database.AggregateDaily(0, thirtyDaysAgo)
	if dailyErr != nil {
		log.Printf("[analytics] Daily aggregation error: %v", dailyErr)
	}

	// Purge raw metrics older than 7 days — but never purge data whose
	// aggregation just failed, or a transient error becomes data loss.
	if hourlyErr != nil {
		log.Println("[analytics] Skipping raw purge because hourly aggregation failed")
	} else if purged, err := database.PurgeRawMetrics(sevenDaysAgo); err != nil {
		log.Printf("[analytics] Raw purge error: %v", err)
	} else if purged > 0 {
		log.Printf("[analytics] Purged %d raw metrics (>7 days)", purged)
	}

	// Purge hourly metrics older than 30 days (already aggregated to daily)
	if dailyErr != nil {
		log.Println("[analytics] Skipping hourly purge because daily aggregation failed")
	} else if purged, err := database.PurgeHourlyMetrics(thirtyDaysAgo); err != nil {
		log.Printf("[analytics] Hourly purge error: %v", err)
	} else if purged > 0 {
		log.Printf("[analytics] Purged %d hourly metrics (>30 days)", purged)
	}

	log.Println("[analytics] Downsampling complete")
}
