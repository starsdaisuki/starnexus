package geoip

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	apiURL    = "http://ip-api.com/json/"
	cacheFile = ".geoip-cache.json"
	// cacheTTL bounds how stale the cached location can get. Without an
	// expiry, a VPS whose public IP changes keeps reporting the old
	// IP/coordinates forever until someone deletes the cache file.
	cacheTTL = 7 * 24 * time.Hour
)

type GeoResult struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	PublicIP  string  `json:"query"`
	City      string  `json:"city"`
	Country   string  `json:"country"`
}

// Detect fetches geolocation from ip-api.com, with a local file cache.
// Returns cached result on subsequent calls to avoid repeated API hits.
func Detect() (*GeoResult, error) {
	// Try cache first
	if cached, err := loadCache(); err == nil {
		return cached, nil
	}

	geo, err := fetchFromAPI()
	if err != nil {
		// A stale location beats no location: fall back to an expired
		// cache when the API is unreachable.
		if cached, cacheErr := loadCacheIgnoringTTL(); cacheErr == nil {
			return cached, nil
		}
		return nil, err
	}

	// Save cache
	_ = saveCache(geo)

	return geo, nil
}

func fetchFromAPI() (*GeoResult, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("geoip request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status  string  `json:"status"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
		Query   string  `json:"query"`
		City    string  `json:"city"`
		Country string  `json:"country"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("geoip decode: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("geoip api returned status: %s", result.Status)
	}

	return &GeoResult{
		Latitude:  result.Lat,
		Longitude: result.Lon,
		PublicIP:  result.Query,
		City:      result.City,
		Country:   result.Country,
	}, nil
}

func loadCache() (*GeoResult, error) {
	info, err := os.Stat(cacheFile)
	if err != nil {
		return nil, err
	}
	if time.Since(info.ModTime()) > cacheTTL {
		return nil, fmt.Errorf("geoip cache expired")
	}
	return loadCacheIgnoringTTL()
}

func loadCacheIgnoringTTL() (*GeoResult, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}
	var geo GeoResult
	if err := json.Unmarshal(data, &geo); err != nil {
		return nil, err
	}
	return &geo, nil
}

func saveCache(geo *GeoResult) error {
	data, err := json.MarshalIndent(geo, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFile, data, 0644)
}
