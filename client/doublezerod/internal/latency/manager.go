package latency

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	defaultProbeInterval        = 30 * time.Second
	defaultDevicesFetchInterval = 30 * time.Second
	defaultFetchTimeout         = 10 * time.Second
	defaultProbeTimeout         = 5 * time.Second
	defaultConcurrency          = 64
)

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type Config struct {
	Logger                      *slog.Logger
	Config                      *config.Config
	NewServiceabilityClientFunc func(rpcURL string, programID solana.PublicKey) ServiceabilityClient
	PingFunc                    func(context.Context, *slog.Logger, serviceability.Device) LatencyResult
	ProbeInterval               time.Duration
	DevicesFetchInterval        time.Duration
	FetchTimeout                time.Duration
	ProbeTimeout                time.Duration
	Concurrency                 int
}

type LatencyResponse struct {
	ProgramID solana.PublicKey `json:"program_id"`
	Results   []LatencyResult  `json:"results"`
}

type LatencyResult struct {
	Min       int64                 `json:"min_latency_ns"`
	Max       int64                 `json:"max_latency_ns"`
	Avg       int64                 `json:"avg_latency_ns"`
	Loss      float64               `json:"loss_percentage"`
	Device    serviceability.Device `json:"-"`
	Reachable bool                  `json:"reachable"`
}

func (l *LatencyResult) MarshalJSON() ([]byte, error) {
	type Alias LatencyResult
	return json.Marshal(&struct {
		DevicePK   string `json:"device_pk"`
		DeviceIP   string `json:"device_ip"`
		DeviceCode string `json:"device_code"`
		*Alias
	}{
		DeviceIP:   net.IP(l.Device.PublicIp[:]).String(),
		DevicePK:   solana.PublicKeyFromBytes(l.Device.PubKey[:]).String(),
		DeviceCode: l.Device.Code,
		Alias:      (*Alias)(l),
	})
}

type Manager struct {
	log *slog.Logger
	cfg Config

	mu             sync.RWMutex
	serviceability ServiceabilityClient
	devices        []serviceability.Device
	results        []LatencyResult
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger required")
	}
	if cfg.Config == nil {
		return nil, fmt.Errorf("config required")
	}
	if cfg.NewServiceabilityClientFunc == nil {
		return nil, fmt.Errorf("NewServiceabilityClientFunc required")
	}
	if cfg.ProbeInterval == 0 {
		cfg.ProbeInterval = defaultProbeInterval
	}
	if cfg.DevicesFetchInterval == 0 {
		cfg.DevicesFetchInterval = defaultDevicesFetchInterval
	}
	if cfg.FetchTimeout == 0 {
		cfg.FetchTimeout = defaultFetchTimeout
	}
	if cfg.ProbeTimeout == 0 {
		cfg.ProbeTimeout = defaultProbeTimeout
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	if cfg.PingFunc == nil {
		cfg.PingFunc = udpPing
	}
	return &Manager{
		log: cfg.Logger.With("component", "latency"),
		cfg: cfg,
	}, nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.log.Info("starting latency manager",
		"probe_interval", m.cfg.ProbeInterval.String(),
		"devices_fetch_interval", m.cfg.DevicesFetchInterval.String(),
		"fetch_timeout", m.cfg.FetchTimeout.String(),
		"probe_timeout", m.cfg.ProbeTimeout.String(),
		"concurrency", m.cfg.Concurrency,
	)

	// Initial prime
	m.recreateServiceabilityClient()
	m.refreshDevices(ctx)
	m.probeDevices(ctx)

	devicesTicker := time.NewTicker(m.cfg.DevicesFetchInterval)
	probeTicker := time.NewTicker(m.cfg.ProbeInterval)
	defer func() { devicesTicker.Stop(); probeTicker.Stop() }()

	for {
		select {
		case <-ctx.Done():
			m.log.Info("shutting down")
			return nil

		case <-m.cfg.Config.Changed():
			if err := m.handleConfigChange(ctx); err != nil {
				m.log.Error("failed to handle config change", "err", err)
			}

		case <-devicesTicker.C:
			m.refreshDevices(ctx)

		case <-probeTicker.C:
			m.probeDevices(ctx)
		}
	}
}

func (m *Manager) ServeLatency(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodHead:
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	m.mu.RLock()
	results := append([]LatencyResult(nil), m.results...)
	m.mu.RUnlock()
	if results == nil {
		results = []LatencyResult{}
	}

	response := LatencyResponse{
		ProgramID: m.cfg.Config.ProgramID(),
		Results:   results,
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		m.log.Error("encode response failed", "err", err, "remote", r.RemoteAddr)
		http.Error(w, fmt.Sprintf("error generating response: %v", err), http.StatusInternalServerError)
	}
}

func (m *Manager) handleConfigChange(ctx context.Context) error {
	m.log.Info("config changed; recreating client")
	m.recreateServiceabilityClient()
	m.refreshDevices(ctx)
	m.probeDevices(ctx)
	return nil
}

// recreateServiceabilityClient always rebuilds the client from the current config.
// It clears devices/results to avoid serving stale data.
// Returns an error if the program id is invalid.
func (m *Manager) recreateServiceabilityClient() {
	rpc := m.cfg.Config.RPCURL()
	programID := m.cfg.Config.ProgramID()

	m.log.Info("creating serviceability client", "rpc", rpc, "program_id", programID)
	svc := m.cfg.NewServiceabilityClientFunc(rpc, programID)

	m.mu.Lock()
	m.serviceability = svc
	m.devices = nil
	m.results = nil
	m.mu.Unlock()
}

func (m *Manager) refreshDevices(ctx context.Context) {
	m.mu.RLock()
	svc := m.serviceability
	timeout := m.cfg.FetchTimeout
	m.mu.RUnlock()

	if svc == nil {
		m.log.Warn("serviceability not set; skipping device fetch")
		return
	}

	m.log.Debug("fetching devices", "timeout", timeout.String())
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pd, err := svc.GetProgramData(ctx)
	if err != nil {
		m.log.Error("failed to fetch program data", "err", err)
		return
	}
	if len(pd.Devices) == 0 {
		m.log.Warn("no devices found")
		return
	}

	m.mu.Lock()
	m.devices = append([]serviceability.Device(nil), pd.Devices...)
	m.mu.Unlock()
}

func (m *Manager) probeDevices(ctx context.Context) {
	m.mu.RLock()
	devs := append([]serviceability.Device(nil), m.devices...)
	timeout := m.cfg.ProbeTimeout
	workers := m.cfg.Concurrency
	m.mu.RUnlock()

	if len(devs) == 0 {
		m.mu.Lock()
		m.results = nil
		m.mu.Unlock()
		return
	}

	if workers < 1 {
		workers = 1
	}
	if workers > len(devs) {
		workers = len(devs)
	}

	local := make([]LatencyResult, len(devs))
	jobs := make(chan int, len(devs))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				probeDevice(ctx, m, devs, i, local, timeout)
			}
		}()
	}

	for i := range devs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	m.mu.Lock()
	m.results = local
	m.mu.Unlock()
}

func probeDevice(ctx context.Context, m *Manager, devs []serviceability.Device, i int, local []LatencyResult, timeout time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("panic in ping", "err", r)
		}
	}()
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m.log.Debug("probing device", "device", devs[i].Code)
	local[i] = m.cfg.PingFunc(subCtx, m.log, devs[i])
}
