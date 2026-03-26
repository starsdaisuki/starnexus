package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/starsdaisuki/starnexus/agent/internal/collector"
	"github.com/starsdaisuki/starnexus/agent/internal/probe"
)

// ConnReport is the payload for connection data.
type ConnReport struct {
	NodeID      string              `json:"node_id"`
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

const bufferCapacity = 120 // 1 hour at 30s intervals

// Report is the payload sent to the server.
type Report struct {
	NodeID    string              `json:"node_id"`
	Name      string              `json:"name"`
	Provider  string              `json:"provider"`
	PublicIP  string              `json:"public_ip,omitempty"`
	Latitude  float64             `json:"latitude"`
	Longitude float64             `json:"longitude"`
	Metrics   collector.Metrics   `json:"metrics"`
	Links     []probe.LinkResult  `json:"links,omitempty"`
}

// Reporter handles sending reports to the server with a ring buffer for failures.
type Reporter struct {
	serverURL string
	token     string
	client    *http.Client

	mu     sync.Mutex
	buffer []Report // ring buffer
	head   int      // next write index
	count  int      // number of buffered items
}

func New(serverURL, token string) *Reporter {
	return &Reporter{
		serverURL: serverURL,
		token:     token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer: make([]Report, bufferCapacity),
	}
}

// Send attempts to send a report. On failure, buffers it. On success, flushes the buffer.
func (r *Reporter) Send(report Report) {
	if err := r.post(report); err != nil {
		log.Printf("Report failed, buffering (%d/%d): %v", r.bufferedCount()+1, bufferCapacity, err)
		r.enqueue(report)
		return
	}

	// Success — flush any buffered reports
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

func (r *Reporter) enqueue(report Report) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buffer[r.head] = report
	r.head = (r.head + 1) % bufferCapacity
	if r.count < bufferCapacity {
		r.count++
	}
}

func (r *Reporter) flush() {
	r.mu.Lock()
	if r.count == 0 {
		r.mu.Unlock()
		return
	}

	// Copy buffered items out
	items := make([]Report, r.count)
	start := (r.head - r.count + bufferCapacity) % bufferCapacity
	for i := 0; i < r.count; i++ {
		items[i] = r.buffer[(start+i)%bufferCapacity]
	}
	r.count = 0
	r.head = 0
	r.mu.Unlock()

	log.Printf("Flushing %d buffered reports", len(items))
	failed := 0
	for _, item := range items {
		if err := r.post(item); err != nil {
			// Re-buffer failures
			r.enqueue(item)
			failed++
		}
	}
	if failed > 0 {
		log.Printf("Flush: %d/%d reports failed, re-buffered", failed, len(items))
	} else {
		log.Printf("Flush: all %d reports sent successfully", len(items))
	}
}

func (r *Reporter) bufferedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
