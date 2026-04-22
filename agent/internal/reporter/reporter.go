package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/starsdaisuki/starnexus/agent/internal/collector"
	"github.com/starsdaisuki/starnexus/agent/internal/probe"
)

// ConnReport is the payload for connection data.
type ConnReport struct {
	NodeID      string               `json:"node_id"`
	Connections []collector.ConnInfo `json:"connections"`
}

// SendConnections posts connection data to the server. No buffering — best effort.
func (r *Reporter) SendConnections(report ConnReport) {
	body, err := json.Marshal(report)
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", r.serverURL+"/api/connections", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// Report is the payload sent to the server.
type Report struct {
	CollectedAt    int64              `json:"collected_at,omitempty"`
	NodeID         string             `json:"node_id"`
	Name           string             `json:"name"`
	Provider       string             `json:"provider"`
	PublicIP       string             `json:"public_ip,omitempty"`
	Latitude       float64            `json:"latitude"`
	Longitude      float64            `json:"longitude"`
	LocationSource string             `json:"location_source,omitempty"`
	Metrics        collector.Metrics  `json:"metrics"`
	Links          []probe.LinkResult `json:"links,omitempty"`
}

type QueueOptions struct {
	Path           string
	MaxReports     int
	FlushBatchSize int
}

// Reporter handles sending reports to the server with optional disk buffering.
type Reporter struct {
	serverURL string
	token     string
	client    *http.Client

	queue          *DiskQueue
	flushBatchSize int
}

func New(serverURL, token string) *Reporter {
	rep, _ := NewWithQueue(serverURL, token, QueueOptions{})
	return rep
}

func NewWithQueue(serverURL, token string, options QueueOptions) (*Reporter, error) {
	rep := &Reporter{
		serverURL: serverURL,
		token:     token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	if options.FlushBatchSize > 0 {
		rep.flushBatchSize = options.FlushBatchSize
	} else {
		rep.flushBatchSize = 120
	}
	if options.Path != "" {
		queue, err := NewDiskQueue(options.Path, options.MaxReports)
		if err != nil {
			return nil, err
		}
		rep.queue = queue
	}
	return rep, nil
}

// Send attempts to send a report. On failure, persists it. On success, flushes persisted reports.
func (r *Reporter) Send(report Report) {
	if err := r.post(report); err != nil {
		if r.queue == nil {
			log.Printf("Report failed and disk queue is disabled: %v", err)
			return
		}
		if queueErr := r.queue.Enqueue(report); queueErr != nil {
			log.Printf("Report failed and queue write failed: send=%v queue=%v", err, queueErr)
			return
		}
		count, _ := r.queue.Count()
		log.Printf("Report failed, persisted to disk queue (%d reports): %v", count, err)
		return
	}

	r.flush()
}

func (r *Reporter) post(report Report) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", r.serverURL+"/api/report", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return nil
}

func (r *Reporter) flush() {
	if r.queue == nil {
		return
	}

	totalSent := 0
	for {
		items, err := r.queue.LoadBatch(r.flushBatchSize)
		if err != nil {
			log.Printf("Queue flush failed while loading: %v", err)
			return
		}
		if len(items) == 0 {
			if totalSent > 0 {
				log.Printf("Flush: all %d queued reports sent successfully", totalSent)
			}
			return
		}

		sent := 0
		for _, item := range items {
			if err := r.post(item); err != nil {
				if sent > 0 {
					if dropErr := r.queue.DropFirst(sent); dropErr != nil {
						log.Printf("Queue flush failed while dropping sent reports: %v", dropErr)
					}
				}
				log.Printf("Flush paused after %d/%d queued reports: %v", totalSent+sent, totalSent+len(items), err)
				return
			}
			sent++
		}
		if err := r.queue.DropFirst(sent); err != nil {
			log.Printf("Queue flush failed while dropping sent reports: %v", err)
			return
		}
		totalSent += sent
	}
}
