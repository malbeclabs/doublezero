package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
)

// FeedConfig describes a single active feed.
type FeedConfig struct {
	// Code is the multicast group code (e.g. "mg01").
	Code string `json:"code"`

	// ParserName selects the parser from the registry (e.g. "topofbook").
	ParserName string `json:"parser"`

	// Format is the output encoding: "json" or "csv".
	Format string `json:"format"`

	// OutputPath is where decoded records are written.
	OutputPath string `json:"output"`

	// MarketdataPort is the UDP port for marketdata messages (quotes, trades).
	MarketdataPort int `json:"marketdata_port"`

	// RefdataPort is the UDP port for refdata messages (instrument defs).
	RefdataPort int `json:"refdata_port"`
}

// FeedStatus reports the state of an active feed.
type FeedStatus struct {
	Code           string `json:"code"`
	ParserName     string `json:"parser"`
	Format         string `json:"format"`
	OutputPath     string `json:"output"`
	RecordsWritten uint64 `json:"records_written"`
	Buffered       int    `json:"buffered"`
	Running        bool   `json:"running"`
}

// Manager manages the lifecycle of edge feed runners.
type Manager struct {
	mu      sync.Mutex
	runners map[string]*Runner

	// multicastIP resolves a group code to its multicast IP.
	multicastIP func(code string) (net.IP, error)

	// isSubscribed reports whether the given multicast IP is in the
	// user's active subscription list. This prevents enabling a feed
	// parser for a group the user has not joined.
	isSubscribed func(groupIP net.IP) bool
}

// NewManager creates a new edge feed manager.
// multicastIPResolver maps a group code to its multicast IP.
// subscriptionChecker reports whether a multicast IP is actively subscribed.
func NewManager(multicastIPResolver func(code string) (net.IP, error), subscriptionChecker func(groupIP net.IP) bool) *Manager {
	if multicastIPResolver == nil {
		multicastIPResolver = func(code string) (net.IP, error) {
			return nil, fmt.Errorf("no multicast IP resolver configured")
		}
	}
	if subscriptionChecker == nil {
		subscriptionChecker = func(net.IP) bool { return false }
	}
	return &Manager{
		runners:      make(map[string]*Runner),
		multicastIP:  multicastIPResolver,
		isSubscribed: subscriptionChecker,
	}
}

// Enable starts a feed runner for the given configuration.
//
// The runner's lifetime is owned by the Manager — it runs until Disable or
// Close is called. Enable deliberately does not accept a caller-supplied
// context: callers like HTTP handlers would pass the per-request context,
// which is cancelled as soon as the handler returns, silently tearing down
// the runner's sockets.
func (m *Manager) Enable(cfg FeedConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runners[cfg.Code]; exists {
		return fmt.Errorf("feed %q is already enabled", cfg.Code)
	}

	if cfg.MarketdataPort <= 0 || cfg.MarketdataPort > 65535 {
		return fmt.Errorf("marketdata_port is required and must be 1-65535 (got %d)", cfg.MarketdataPort)
	}
	if cfg.RefdataPort <= 0 || cfg.RefdataPort > 65535 {
		return fmt.Errorf("refdata_port is required and must be 1-65535 (got %d)", cfg.RefdataPort)
	}
	if cfg.MarketdataPort == cfg.RefdataPort {
		return fmt.Errorf("marketdata_port and refdata_port must differ (got %d)", cfg.MarketdataPort)
	}

	parser, ok := NewParser(cfg.ParserName)
	if !ok {
		return fmt.Errorf("unknown parser %q; available: %v", cfg.ParserName, RegisteredParsers())
	}

	sink, err := NewSink(SinkConfig{Format: cfg.Format, Path: cfg.OutputPath})
	if err != nil {
		return fmt.Errorf("creating output sink: %w", err)
	}

	groupIP, err := m.multicastIP(cfg.Code)
	if err != nil {
		return fmt.Errorf("resolving multicast IP for %q: %w", cfg.Code, err)
	}

	if !m.isSubscribed(groupIP) {
		return fmt.Errorf("not subscribed to multicast group %q — run 'doublezero connect multicast subscriber' first", cfg.Code)
	}

	runner := NewRunner(RunnerConfig{
		Code:           cfg.Code,
		GroupIP:        groupIP,
		MarketdataPort: cfg.MarketdataPort,
		RefdataPort:    cfg.RefdataPort,
		Format:         cfg.Format,
		OutputPath:     cfg.OutputPath,
		Parser:         parser,
		Sink:           sink,
	})

	runCtx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel

	go func() {
		if err := runner.Run(runCtx); err != nil && runCtx.Err() == nil {
			slog.Error("edge: feed runner exited with error", "code", cfg.Code, "error", err)
		}
	}()

	m.runners[cfg.Code] = runner
	slog.Info("edge: feed enabled", "code", cfg.Code, "parser", cfg.ParserName, "format", cfg.Format, "output", cfg.OutputPath)
	return nil
}

// Disable stops and removes the feed runner for the given group code.
func (m *Manager) Disable(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runner, exists := m.runners[code]
	if !exists {
		return fmt.Errorf("feed %q is not enabled", code)
	}

	runner.Stop()
	delete(m.runners, code)
	slog.Info("edge: feed disabled", "code", code)
	return nil
}

// Status returns the status of all active feeds.
func (m *Manager) Status() []FeedStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	statuses := make([]FeedStatus, 0, len(m.runners))
	for _, r := range m.runners {
		statuses = append(statuses, r.Status())
	}
	return statuses
}

// Close stops all active feed runners.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for code, runner := range m.runners {
		runner.Stop()
		slog.Info("edge: feed stopped", "code", code)
	}
	m.runners = make(map[string]*Runner)
}

// ServeEnable handles POST /edge/enable requests.
func (m *Manager) ServeEnable(w http.ResponseWriter, r *http.Request) {
	var cfg FeedConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":      "error",
			"description": fmt.Sprintf("malformed request: %v", err),
		})
		return
	}

	if cfg.Code == "" || cfg.ParserName == "" || cfg.Format == "" || cfg.OutputPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":      "error",
			"description": "code, parser, format, and output are required",
		})
		return
	}

	if err := m.Enable(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":      "error",
			"description": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ServeDisable handles POST /edge/disable requests.
func (m *Manager) ServeDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":      "error",
			"description": "code is required",
		})
		return
	}

	if err := m.Disable(req.Code); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":      "error",
			"description": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ServeStatus handles GET /edge/status requests.
func (m *Manager) ServeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, m.Status())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
