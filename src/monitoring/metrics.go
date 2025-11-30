package monitoring

import (
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	initOnce sync.Once

	messagesTotal    *prometheus.CounterVec
	charactersTotal  *prometheus.CounterVec
	agentActive      *prometheus.GaugeVec
	agentRunsTotal   *prometheus.CounterVec
	wsClients        *prometheus.GaugeVec
	agentQueueDepth  *prometheus.GaugeVec
	agentRunDuration *prometheus.HistogramVec
)

func initRegistry() {
	messagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "replychat",
			Name:      "messages_total",
			Help:      "Count of messages persisted, labeled by project/sender/message type.",
		},
		[]string{"project_id", "sender_type", "sender_id", "message_type"},
	)

	charactersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "replychat",
			Name:      "characters_total",
			Help:      "Total Unicode characters contained in delivered messages.",
		},
		[]string{"project_id", "sender_type", "sender_id", "message_type"},
	)

	agentActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "replychat",
			Name:      "agent_active",
			Help:      "Current number of agent jobs executing per project/agent.",
		},
		[]string{"project_id", "agent_id"},
	)

	agentRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "replychat",
			Name:      "agent_runs_total",
			Help:      "Total agent jobs executed per project/agent.",
		},
		[]string{"project_id", "agent_id"},
	)

	wsClients = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "replychat",
			Name:      "ws_clients",
			Help:      "Active WebSocket connections per project.",
		},
		[]string{"project_id"},
	)

	agentQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "replychat",
			Name:      "agent_queue_depth",
			Help:      "Queued tasks awaiting each agent per project.",
		},
		[]string{"project_id", "agent_id"},
	)

	agentRunDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "replychat",
			Name:      "agent_run_duration_seconds",
			Help:      "Wall-clock duration of individual agent runs.",
			Buckets:   []float64{5, 10, 20, 40, 80, 160, 320, 600},
		},
		[]string{"project_id", "agent_id"},
	)

	prometheus.MustRegister(
		messagesTotal,
		charactersTotal,
		agentActive,
		agentRunsTotal,
		wsClients,
		agentQueueDepth,
		agentRunDuration,
	)
}

func ensureInit() {
	initOnce.Do(initRegistry)
}

// Handler exposes the Prometheus metrics endpoint.
func Handler() http.Handler {
	ensureInit()
	return promhttp.Handler()
}

// RecordMessage increments the counters that power Grafana dashboards.
func RecordMessage(projectID, senderType, senderID, messageType, content string) {
	ensureInit()
	labels := prometheus.Labels{
		"project_id":   normalizeProject(projectID),
		"sender_type":  sanitize(senderType, "unknown"),
		"sender_id":    sanitize(senderID, "anonymous"),
		"message_type": sanitize(messageType, "unknown"),
	}
	messagesTotal.With(labels).Inc()
	charactersTotal.With(labels).Add(float64(utf8.RuneCountInString(content)))
}

// AgentWorkStarted marks an agent job as actively running.
func AgentWorkStarted(projectID, agentID string) {
	ensureInit()
	agentActive.WithLabelValues(normalizeProject(projectID), sanitize(agentID, "unknown")).Inc()
}

// AgentWorkCompleted decrements the activity gauge and bumps the run counter.
func AgentWorkCompleted(projectID, agentID string) {
	ensureInit()
	labels := []string{normalizeProject(projectID), sanitize(agentID, "unknown")}
	agentActive.WithLabelValues(labels...).Dec()
	agentRunsTotal.WithLabelValues(labels...).Inc()
}

// RecordAgentDuration tracks how long an agent run took.
func RecordAgentDuration(projectID, agentID string, duration time.Duration) {
	ensureInit()
	agentRunDuration.WithLabelValues(normalizeProject(projectID), sanitize(agentID, "unknown")).
		Observe(duration.Seconds())
}

// WSClientConnected increments the WebSocket client gauge.
func WSClientConnected(projectID string) {
	ensureInit()
	wsClients.WithLabelValues(normalizeProject(projectID)).Inc()
}

// WSClientDisconnected decrements the WebSocket client gauge.
func WSClientDisconnected(projectID string) {
	ensureInit()
	wsClients.WithLabelValues(normalizeProject(projectID)).Dec()
}

// SetAgentQueueDepth records the current queue depth for an agent.
func SetAgentQueueDepth(projectID, agentID string, depth int) {
	ensureInit()
	agentQueueDepth.WithLabelValues(normalizeProject(projectID), sanitize(agentID, "unknown")).
		Set(float64(depth))
}

func normalizeProject(projectID string) string {
	return sanitize(projectID, "default")
}

func sanitize(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
