package monitor

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/starsdaisuki/starnexus/bot/internal/telegram"
)

// Node matches the server's /api/nodes response shape.
type Node struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Provider string       `json:"provider"`
	Status   string       `json:"status"`
	Metrics  *NodeMetrics `json:"metrics,omitempty"`
}

type NodeMetrics struct {
	CPUPercent    *float64 `json:"cpu_percent"`
	MemoryPercent *float64 `json:"memory_percent"`
	DiskPercent   *float64 `json:"disk_percent"`
	BandwidthDown *float64 `json:"bandwidth_down"`
	LoadAvg       *float64 `json:"load_avg"`
	Connections   *int     `json:"connections"`
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

type dashboardResponse struct {
	Status      statusResponse        `json:"status"`
	Nodes       []Node                `json:"nodes"`
	Events      []eventSummary        `json:"events"`
	Fleet       fleetAnalytics        `json:"fleet_analytics"`
	Reliability reliabilityAnalytics  `json:"reliability_analytics"`
	GroundTruth *groundTruthAnalytics `json:"ground_truth,omitempty"`
}

type fleetAnalytics struct {
	Summary      string             `json:"summary"`
	NodeInsights []fleetNodeInsight `json:"node_insights"`
}

type fleetNodeInsight struct {
	NodeID    string  `json:"node_id"`
	NodeName  string  `json:"node_name"`
	RiskLevel string  `json:"risk_level"`
	Score     float64 `json:"composite_score"`
	Summary   string  `json:"summary"`
}

type reliabilityAnalytics struct {
	FleetOperationalScore float64           `json:"fleet_operational_score"`
	FleetAvailability     float64           `json:"fleet_availability_percent"`
	FleetDataCoverage     float64           `json:"fleet_data_coverage_percent"`
	IncidentCount         int               `json:"incident_count"`
	SignalEventCount      int               `json:"signal_event_count"`
	ExperimentEventCount  int               `json:"experiment_event_count"`
	Summary               string            `json:"summary"`
	Nodes                 []reliabilityNode `json:"nodes"`
}

type reliabilityNode struct {
	NodeID               string   `json:"node_id"`
	NodeName             string   `json:"node_name"`
	Status               string   `json:"status"`
	OperationalScore     float64  `json:"operational_score"`
	AvailabilityPercent  float64  `json:"availability_percent"`
	DataCoveragePercent  float64  `json:"data_coverage_percent"`
	IncidentCount        int      `json:"incident_count"`
	SignalEventCount     int      `json:"signal_event_count"`
	ExperimentEventCount int      `json:"experiment_event_count"`
	DataQuality          string   `json:"data_quality"`
	Recommendation       string   `json:"recommendation"`
	Signals              []string `json:"signals"`
}

type groundTruthAnalytics struct {
	ExperimentCount       int     `json:"experiment_count"`
	DetectionRatePercent  float64 `json:"detection_rate_percent"`
	MeanDetectionDelaySec float64 `json:"mean_detection_delay_seconds"`
	RecoveryRatePercent   float64 `json:"recovery_rate_percent"`
	MeanRecoveryDelaySec  float64 `json:"mean_recovery_delay_seconds"`
}

type eventSummary struct {
	NodeID    *string `json:"node_id,omitempty"`
	NodeName  *string `json:"node_name,omitempty"`
	Type      string  `json:"type"`
	Severity  string  `json:"severity"`
	Title     string  `json:"title"`
	Body      *string `json:"body,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

type nodeDetailsResponse struct {
	Node  Node `json:"node"`
	Score *struct {
		CompositeScore float64 `json:"composite_score"`
		Availability   float64 `json:"availability"`
		Stability      float64 `json:"stability"`
	} `json:"score,omitempty"`
	Analytics *struct {
		RiskLevel  string   `json:"risk_level"`
		Summary    string   `json:"summary"`
		Highlights []string `json:"highlights"`
	} `json:"analytics,omitempty"`
	Events []eventSummary `json:"events"`
}

type chatPreference struct {
	Subscribed         bool  `json:"subscribed"`
	MutedUntil         int64 `json:"muted_until"`
	DailySummary       bool  `json:"daily_summary"`
	LastDailySummaryAt int64 `json:"last_daily_summary_at,omitempty"`
}

type preferenceState struct {
	Chats map[string]chatPreference `json:"chats"`
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

	prefMu    sync.Mutex
	prefs     map[int64]chatPreference
	statePath string
}

func New(serverURL, apiToken string, bot *telegram.Bot) *Monitor {
	monitor := &Monitor{
		serverURL:  serverURL,
		token:      apiToken,
		bot:        bot,
		client:     &http.Client{Timeout: 10 * time.Second},
		lastStatus: make(map[string]string),
		prefs:      make(map[int64]chatPreference),
		statePath:  "starnexus-bot-state.json",
	}
	monitor.loadPreferences()
	return monitor
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

// StartDailySummary sends one analytics summary per subscribed chat near 09:00 UTC+8.
func (m *Monitor) StartDailySummary(intervalSeconds int, stop <-chan struct{}) {
	if intervalSeconds <= 0 {
		intervalSeconds = 3600
	}
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.dailySummary()
		case <-stop:
			return
		}
	}
}

// HandleCommand processes a /command and returns the reply text.
func (m *Monitor) HandleCommand(chatID int64, command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	cmd := normalizeCommand(fields[0])

	switch cmd {
	case "/status":
		return m.cmdStatus()
	case "/report":
		return m.cmdReport()
	case "/analytics":
		return m.cmdAnalytics()
	case "/events":
		return m.cmdEvents()
	case "/node":
		return m.cmdNode(fields[1:])
	case "/mute":
		return m.cmdMute(chatID, fields[1:])
	case "/unmute":
		return m.cmdUnmute(chatID)
	case "/subscribe":
		return m.cmdSubscribe(chatID)
	case "/unsubscribe":
		return m.cmdUnsubscribe(chatID)
	case "/daily":
		return m.cmdDaily(chatID, fields[1:])
	case "/prefs":
		return m.cmdPrefs(chatID)
	case "/help":
		return m.cmdHelp()
	case "/start":
		return m.cmdHelp()
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
		if err := m.sendAlert(msg); err != nil {
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
			if sendErr := m.sendAlert(msg); sendErr != nil {
				log.Printf("Failed to send heartbeat alert: %v", sendErr)
			}
			m.heartbeatAlerted = true
		}
		return
	}

	// Server is reachable
	if m.heartbeatAlerted {
		msg := "\xf0\x9f\x9f\xa2 <b>Server recovered.</b>\nHeartbeat restored."
		if sendErr := m.sendAlert(msg); sendErr != nil {
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

// --- /analytics command ---

func (m *Monitor) cmdAnalytics() string {
	dashboard, err := m.fetchDashboard()
	if err != nil {
		return fmt.Sprintf("Failed to fetch dashboard analytics: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("<b>StarNexus Analytics</b>\n")
	sb.WriteString(fmt.Sprintf(
		"Fleet: %d total | %d online | %d degraded | %d offline\n",
		dashboard.Status.Total,
		dashboard.Status.Online,
		dashboard.Status.Degraded,
		dashboard.Status.Offline,
	))
	sb.WriteString(fmt.Sprintf(
		"Reliability: %.0f/100 | coverage %.0f%% | incidents %d | signals %d\n",
		dashboard.Reliability.FleetOperationalScore,
		dashboard.Reliability.FleetDataCoverage,
		dashboard.Reliability.IncidentCount,
		dashboard.Reliability.SignalEventCount,
	))
	if dashboard.GroundTruth != nil && dashboard.GroundTruth.ExperimentCount > 0 {
		sb.WriteString(fmt.Sprintf(
			"Experiments: %d | detection %.0f%% | mean delay %s\n",
			dashboard.GroundTruth.ExperimentCount,
			dashboard.GroundTruth.DetectionRatePercent,
			formatSeconds(dashboard.GroundTruth.MeanDetectionDelaySec),
		))
	}

	if dashboard.Fleet.Summary != "" {
		sb.WriteString("\n")
		sb.WriteString(escapeHTML(dashboard.Fleet.Summary))
		sb.WriteString("\n")
	}

	if len(dashboard.Reliability.Nodes) > 0 {
		sb.WriteString("\n<b>Top Watch</b>\n")
		for _, node := range dashboard.Reliability.Nodes[:minInt(3, len(dashboard.Reliability.Nodes))] {
			sb.WriteString(fmt.Sprintf(
				"%s <b>%s</b>: %.0f/100, %s, %d incident(s), %d signal(s)\n",
				statusIcon(node.Status),
				escapeHTML(node.NodeName),
				node.OperationalScore,
				escapeHTML(node.DataQuality),
				node.IncidentCount,
				node.SignalEventCount,
			))
			if node.Recommendation != "" {
				sb.WriteString(fmt.Sprintf("  %s\n", escapeHTML(node.Recommendation)))
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// --- /events command ---

func (m *Monitor) cmdEvents() string {
	dashboard, err := m.fetchDashboard()
	if err != nil {
		return fmt.Sprintf("Failed to fetch events: %v", err)
	}
	if len(dashboard.Events) == 0 {
		return "No recent events recorded."
	}

	var sb strings.Builder
	sb.WriteString("<b>Recent Events</b>\n")
	for _, event := range dashboard.Events[:minInt(6, len(dashboard.Events))] {
		nodeName := "system"
		if event.NodeName != nil && *event.NodeName != "" {
			nodeName = *event.NodeName
		} else if event.NodeID != nil && *event.NodeID != "" {
			nodeName = *event.NodeID
		}
		sb.WriteString(fmt.Sprintf(
			"%s <b>%s</b> [%s]\n%s\n",
			severityIcon(event.Severity),
			escapeHTML(nodeName),
			escapeHTML(event.Severity),
			escapeHTML(event.Title),
		))
	}
	return strings.TrimSpace(sb.String())
}

// --- /node command ---

func (m *Monitor) cmdNode(args []string) string {
	if len(args) == 0 {
		return "Usage: /node <node-id-or-name>"
	}

	query := strings.ToLower(strings.Join(args, " "))
	nodes, err := m.fetchNodes()
	if err != nil {
		return fmt.Sprintf("Failed to fetch nodes: %v", err)
	}

	var match *Node
	for i := range nodes {
		if strings.EqualFold(nodes[i].ID, query) || strings.Contains(strings.ToLower(nodes[i].Name), query) {
			match = &nodes[i]
			break
		}
	}
	if match == nil {
		return fmt.Sprintf("No node matched %q.", query)
	}

	detail, err := m.fetchNodeDetails(match.ID)
	if err != nil {
		return fmt.Sprintf("Failed to fetch node details: %v", err)
	}

	var sb strings.Builder
	node := detail.Node
	sb.WriteString(fmt.Sprintf("%s <b>%s</b> (%s)\n", statusIcon(node.Status), escapeHTML(node.Name), escapeHTML(node.Provider)))
	sb.WriteString(fmt.Sprintf("ID: <code>%s</code>\n", escapeHTML(node.ID)))
	if node.Metrics != nil {
		sb.WriteString(fmt.Sprintf(
			"CPU %s | Mem %s | Disk %s | Down %s | Conn %s\n",
			formatPercentPtr(node.Metrics.CPUPercent),
			formatPercentPtr(node.Metrics.MemoryPercent),
			formatPercentPtr(node.Metrics.DiskPercent),
			formatFloatPtr(node.Metrics.BandwidthDown, "KB/s"),
			formatIntPtr(node.Metrics.Connections),
		))
	}
	if detail.Score != nil {
		sb.WriteString(fmt.Sprintf("Score %.0f/100 | availability %.0f%% | stability %.0f%%\n", detail.Score.CompositeScore, detail.Score.Availability, detail.Score.Stability))
	}
	if detail.Analytics != nil {
		sb.WriteString(fmt.Sprintf("Risk: %s\n%s\n", escapeHTML(detail.Analytics.RiskLevel), escapeHTML(detail.Analytics.Summary)))
		if len(detail.Analytics.Highlights) > 0 {
			sb.WriteString(fmt.Sprintf("Top signal: %s\n", escapeHTML(detail.Analytics.Highlights[0])))
		}
	}
	if len(detail.Events) > 0 {
		sb.WriteString(fmt.Sprintf("Recent event: %s\n", escapeHTML(detail.Events[0].Title)))
	}
	return strings.TrimSpace(sb.String())
}

// --- /help command ---

func (m *Monitor) cmdHelp() string {
	return strings.Join([]string{
		"<b>StarNexus Bot</b>",
		"/status - fleet status and nodes",
		"/analytics - reliability, anomaly, experiment summary",
		"/events - latest events",
		"/node &lt;id-or-name&gt; - node detail summary",
		"/mute [30m|2h|1d] - pause proactive alerts for this chat",
		"/unmute - resume proactive alerts",
		"/subscribe - enable proactive alerts",
		"/unsubscribe - disable proactive alerts",
		"/daily on|off - toggle daily analytics summary",
		"/prefs - show this chat's alert preferences",
		"/report - daily AI report",
	}, "\n")
}

// --- Preference commands ---

func (m *Monitor) cmdMute(chatID int64, args []string) string {
	duration := time.Hour
	if len(args) > 0 {
		parsed, err := parseMuteDuration(args[0])
		if err != nil {
			return "Usage: /mute [30m|2h|1d]"
		}
		duration = parsed
	}
	if duration > 7*24*time.Hour {
		duration = 7 * 24 * time.Hour
	}

	pref := m.preference(chatID)
	pref.MutedUntil = time.Now().Add(duration).Unix()
	m.setPreference(chatID, pref)
	return fmt.Sprintf("Muted proactive alerts for %s. Commands still work.", formatDuration(duration))
}

func (m *Monitor) cmdUnmute(chatID int64) string {
	pref := m.preference(chatID)
	pref.MutedUntil = 0
	m.setPreference(chatID, pref)
	return "Proactive alerts resumed for this chat."
}

func (m *Monitor) cmdSubscribe(chatID int64) string {
	pref := m.preference(chatID)
	pref.Subscribed = true
	m.setPreference(chatID, pref)
	return "This chat is subscribed to proactive StarNexus alerts."
}

func (m *Monitor) cmdUnsubscribe(chatID int64) string {
	pref := m.preference(chatID)
	pref.Subscribed = false
	m.setPreference(chatID, pref)
	return "This chat is unsubscribed from proactive alerts. Commands still work."
}

func (m *Monitor) cmdDaily(chatID int64, args []string) string {
	if len(args) == 0 {
		pref := m.preference(chatID)
		if pref.DailySummary {
			return "Daily analytics summary is on. Use /daily off to disable it."
		}
		return "Daily analytics summary is off. Use /daily on to enable it."
	}

	pref := m.preference(chatID)
	switch strings.ToLower(args[0]) {
	case "on", "yes", "true", "1":
		pref.DailySummary = true
		m.setPreference(chatID, pref)
		return "Daily analytics summary enabled for this chat."
	case "off", "no", "false", "0":
		pref.DailySummary = false
		m.setPreference(chatID, pref)
		return "Daily analytics summary disabled for this chat."
	default:
		return "Usage: /daily on|off"
	}
}

func (m *Monitor) cmdPrefs(chatID int64) string {
	pref := m.preference(chatID)
	muted := "no"
	if pref.MutedUntil > time.Now().Unix() {
		muted = fmt.Sprintf("until %s", time.Unix(pref.MutedUntil, 0).Format(time.RFC3339))
	}
	return fmt.Sprintf(
		"<b>StarNexus Preferences</b>\nSubscribed: %t\nMuted: %s\nDaily summary: %t",
		pref.Subscribed,
		muted,
		pref.DailySummary,
	)
}

// --- /report command ---

func (m *Monitor) cmdReport() string {
	req, err := http.NewRequest("GET", m.serverURL+"/api/daily-report", nil)
	if err != nil {
		return fmt.Sprintf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch report: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Server returned %d", resp.StatusCode)
	}

	var data struct {
		Report string `json:"report"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Sprintf("Failed to decode report: %v", err)
	}
	return data.Report
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

func (m *Monitor) fetchDashboard() (*dashboardResponse, error) {
	req, err := http.NewRequest("GET", m.serverURL+"/api/dashboard", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var data dashboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (m *Monitor) fetchNodeDetails(nodeID string) (*nodeDetailsResponse, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/nodes/%s/details?hours=24", m.serverURL, nodeID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var data nodeDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (m *Monitor) dailySummary() {
	now := time.Now().UTC()
	windowStart := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	if now.Before(windowStart) {
		return
	}

	message := ""
	for _, chatID := range m.bot.ChatIDs() {
		pref := m.preference(chatID)
		if !pref.Subscribed || !pref.DailySummary || pref.MutedUntil > time.Now().Unix() || pref.LastDailySummaryAt >= windowStart.Unix() {
			continue
		}
		if message == "" {
			message = m.cmdAnalytics()
		}
		if err := m.bot.SendMessageTo(chatID, "<b>StarNexus Daily Summary</b>\n"+message); err != nil {
			log.Printf("Failed to send daily summary to %d: %v", chatID, err)
			continue
		}
		pref.LastDailySummaryAt = time.Now().Unix()
		m.setPreference(chatID, pref)
	}
}

func (m *Monitor) sendAlert(text string) error {
	var lastErr error
	for _, chatID := range m.bot.ChatIDs() {
		if !m.shouldDeliverAlert(chatID) {
			continue
		}
		if err := m.bot.SendMessageTo(chatID, text); err != nil {
			log.Printf("send alert to %d failed: %v", chatID, err)
			lastErr = err
		}
	}
	return lastErr
}

func (m *Monitor) shouldDeliverAlert(chatID int64) bool {
	pref := m.preference(chatID)
	return pref.Subscribed && pref.MutedUntil <= time.Now().Unix()
}

func (m *Monitor) preference(chatID int64) chatPreference {
	m.prefMu.Lock()
	defer m.prefMu.Unlock()
	if pref, ok := m.prefs[chatID]; ok {
		return pref
	}
	return defaultPreference()
}

func (m *Monitor) setPreference(chatID int64, pref chatPreference) {
	m.prefMu.Lock()
	m.prefs[chatID] = pref
	m.prefMu.Unlock()
	m.savePreferences()
}

func defaultPreference() chatPreference {
	return chatPreference{
		Subscribed:   true,
		DailySummary: true,
	}
}

func (m *Monitor) loadPreferences() {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read bot preference state: %v", err)
		}
		return
	}

	var state preferenceState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("Failed to decode bot preference state: %v", err)
		return
	}

	m.prefMu.Lock()
	defer m.prefMu.Unlock()
	for key, pref := range state.Chats {
		chatID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}
		m.prefs[chatID] = pref
	}
}

func (m *Monitor) savePreferences() {
	m.prefMu.Lock()
	state := preferenceState{Chats: make(map[string]chatPreference, len(m.prefs))}
	for chatID, pref := range m.prefs {
		state.Chats[strconv.FormatInt(chatID, 10)] = pref
	}
	m.prefMu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("Failed to encode bot preference state: %v", err)
		return
	}
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Printf("Failed to write bot preference state: %v", err)
		return
	}
	if err := os.Rename(tmp, m.statePath); err != nil {
		log.Printf("Failed to replace bot preference state: %v", err)
	}
}

func normalizeCommand(command string) string {
	command = strings.ToLower(command)
	if at := strings.Index(command, "@"); at >= 0 {
		command = command[:at]
	}
	return command
}

func escapeHTML(value string) string {
	return html.EscapeString(value)
}

func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "\xf0\x9f\x94\xb4"
	case "warning":
		return "\xf0\x9f\x9f\xa1"
	default:
		return "\xe2\x9a\xaa"
	}
}

func formatPercentPtr(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.0f%%", *value)
}

func formatFloatPtr(value *float64, unit string) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1f %s", *value, unit)
}

func formatIntPtr(value *int) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%d", *value)
}

func formatSeconds(value float64) string {
	if value <= 0 {
		return "--"
	}
	if value < 60 {
		return fmt.Sprintf("%.0fs", value)
	}
	return fmt.Sprintf("%.1fm", value/60)
}

func parseMuteDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func formatDuration(value time.Duration) string {
	if value >= 24*time.Hour && value%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(value/(24*time.Hour)))
	}
	if value >= time.Hour && value%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(value/time.Hour))
	}
	if value >= time.Minute && value%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(value/time.Minute))
	}
	return value.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
