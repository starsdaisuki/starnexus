package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/starsdaisuki/starnexus/bot/internal/telegram"
)

// Node matches the server's /api/nodes response shape.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

type nodesResponse struct {
	Nodes []Node `json:"nodes"`
}

type statusResponse struct {
	Total    int `json:"total"`
	Online   int `json:"online"`
	Degraded int `json:"degraded"`
	Offline  int `json:"offline"`
	Unknown  int `json:"unknown"`
}

// Monitor polls the server and sends Telegram alerts on status changes.
type Monitor struct {
	serverURL string
	token     string
	bot       *telegram.Bot
	client    *http.Client

	// Track last known status per node for change detection
	lastStatus map[string]string

	// Heartbeat state
	heartbeatFailures int
	heartbeatAlerted  bool
}

func New(serverURL, apiToken string, bot *telegram.Bot) *Monitor {
	return &Monitor{
		serverURL:  serverURL,
		token:      apiToken,
		bot:        bot,
		client:     &http.Client{Timeout: 10 * time.Second},
		lastStatus: make(map[string]string),
	}
}

// StartPolling checks for status changes every interval. Blocks until stop is closed.
func (m *Monitor) StartPolling(intervalSeconds int, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	// Initial poll
	m.poll()

	for {
		select {
		case <-ticker.C:
			m.poll()
		case <-stop:
			return
		}
	}
}

// StartHeartbeat pings the server every interval. Alerts after 3 consecutive failures. Blocks until stop.
func (m *Monitor) StartHeartbeat(intervalSeconds int, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.heartbeat()
		case <-stop:
			return
		}
	}
}

// HandleCommand processes a /command and returns the reply text.
func (m *Monitor) HandleCommand(command string) string {
	cmd := strings.Fields(command)[0]

	switch cmd {
	case "/status":
		return m.cmdStatus()
	case "/start":
		return "StarNexus Bot is running."
	default:
		return ""
	}
}

// --- Polling ---

func (m *Monitor) poll() {
	nodes, err := m.fetchNodes()
	if err != nil {
		log.Printf("Poll error: %v", err)
		return
	}

	for _, node := range nodes {
		old, known := m.lastStatus[node.ID]
		m.lastStatus[node.ID] = node.Status

		if !known {
			// First time seeing this node, don't alert
			continue
		}

		if old == node.Status {
			continue
		}

		// Status changed — send alert
		msg := m.formatStatusChange(node, old)
		log.Printf("Status change: %s %s -> %s", node.ID, old, node.Status)
		if err := m.bot.SendMessage(msg); err != nil {
			log.Printf("Failed to send alert: %v", err)
		}
	}
}

func (m *Monitor) formatStatusChange(node Node, oldStatus string) string {
	icon := statusIcon(node.Status)
	return fmt.Sprintf(
		"%s <b>%s</b> (%s)\n%s → %s",
		icon, node.Name, node.Provider,
		statusLabel(oldStatus), statusLabel(node.Status),
	)
}

func statusIcon(status string) string {
	switch status {
	case "online":
		return "\xf0\x9f\x9f\xa2" // green circle
	case "degraded":
		return "\xf0\x9f\x9f\xa1" // yellow circle
	case "offline":
		return "\xf0\x9f\x94\xb4" // red circle
	default:
		return "\xe2\xac\x9c" // white circle
	}
}

func statusLabel(status string) string {
	switch status {
	case "online":
		return "Online"
	case "degraded":
		return "Degraded"
	case "offline":
		return "Offline"
	default:
		return "Unknown"
	}
}

// --- Heartbeat ---

func (m *Monitor) heartbeat() {
	_, err := m.fetchStatus()
	if err != nil {
		m.heartbeatFailures++
		log.Printf("Heartbeat failed (%d consecutive): %v", m.heartbeatFailures, err)

		if m.heartbeatFailures >= 3 && !m.heartbeatAlerted {
			msg := "\xf0\x9f\x94\xb4 <b>Server unreachable!</b>\n3 consecutive heartbeat failures."
			if sendErr := m.bot.SendMessage(msg); sendErr != nil {
				log.Printf("Failed to send heartbeat alert: %v", sendErr)
			}
			m.heartbeatAlerted = true
		}
		return
	}

	// Server is reachable
	if m.heartbeatAlerted {
		msg := "\xf0\x9f\x9f\xa2 <b>Server recovered.</b>\nHeartbeat restored."
		if sendErr := m.bot.SendMessage(msg); sendErr != nil {
			log.Printf("Failed to send recovery alert: %v", sendErr)
		}
	}
	m.heartbeatFailures = 0
	m.heartbeatAlerted = false
}

// --- /status command ---

func (m *Monitor) cmdStatus() string {
	status, err := m.fetchStatus()
	if err != nil {
		return fmt.Sprintf("Failed to fetch status: %v", err)
	}

	nodes, err := m.fetchNodes()
	if err != nil {
		return fmt.Sprintf("Failed to fetch nodes: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"<b>StarNexus Status</b>\nTotal: %d | Online: %d | Degraded: %d | Offline: %d\n\n",
		status.Total, status.Online, status.Degraded, status.Offline,
	))

	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("%s %s (%s)\n", statusIcon(n.Status), n.Name, n.Provider))
	}

	return sb.String()
}

// --- HTTP helpers ---

func (m *Monitor) fetchNodes() ([]Node, error) {
	resp, err := m.client.Get(m.serverURL + "/api/nodes")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var data nodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Nodes, nil
}

func (m *Monitor) fetchStatus() (*statusResponse, error) {
	resp, err := m.client.Get(m.serverURL + "/api/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var data statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}
