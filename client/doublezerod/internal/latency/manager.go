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

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	probing "github.com/prometheus-community/pro-bing"
)

const (
	serviceabilityProgramDataFetchTimeout = 20 * time.Second
)

// DeviceInfo contains the minimal device information needed for latency probing and reporting.
// This is a memory-optimized subset of serviceability.Device, storing only the fields
// required for metrics labels, JSON output, and IP fallback.
type DeviceInfo struct {
	PubKey [32]byte
	Code   string
	// PublicIp uses [4]uint8 (matching serviceability.Device) rather than net.IP for two reasons:
	// 1. Memory efficiency: [4]uint8 is 4 bytes inline vs net.IP which is a slice (24 byte header + heap allocation)
	// 2. Direct assignment: allows zero-copy from serviceability.Device.PublicIp without conversion
	// Convert to net.IP when needed via: net.IP(PublicIp[:])
	PublicIp [4]uint8
}

// ProbeTarget represents a single IP to probe, associated with a device and interface.
type ProbeTarget struct {
	Device        DeviceInfo
	InterfaceName string // Empty string indicates device PublicIp
	IP            net.IP
}

// GetProbeTargets returns probe targets for a device.
// When PublicIp is valid (non-unspecified), it is always included.
// When probeTunnelEndpoints is true, also includes any interfaces with UserTunnelEndpoint=true.
// Returns empty slice if device has no valid probe targets.
func GetProbeTargets(d serviceability.Device, probeTunnelEndpoints bool) []ProbeTarget {
	var targets []ProbeTarget

	// Extract minimal device info to avoid storing full Device struct
	deviceInfo := DeviceInfo{
		PubKey:   d.PubKey,
		Code:     d.Code,
		PublicIp: d.PublicIp,
	}

	// Always probe the device's PublicIp if it's a valid (non-unspecified) address
	publicIP := net.IP(d.PublicIp[:])
	if !publicIP.IsUnspecified() {
		targets = append(targets, ProbeTarget{
			Device:        deviceInfo,
			InterfaceName: "", // Empty indicates device PublicIp
			IP:            publicIP,
		})
	}

	// Only probe UserTunnelEndpoint interfaces when the feature flag is enabled
	if !probeTunnelEndpoints {
		return targets
	}

	// Additionally probe any user tunnel endpoint interfaces
	for _, iface := range d.Interfaces {
		if !iface.UserTunnelEndpoint {
			continue
		}
		// IpNet: first 4 bytes are IP, 5th is prefix length
		if iface.IpNet[4] == 0 || iface.IpNet[4] > 32 {
			continue
		}
		ip := net.IP(iface.IpNet[:4])
		if ip.IsUnspecified() {
			continue
		}
		// Skip if same as PublicIp (avoid duplicate probes)
		if ip.Equal(publicIP) {
			continue
		}
		targets = append(targets, ProbeTarget{
			Device:        deviceInfo,
			InterfaceName: iface.Name,
			IP:            ip,
		})
	}

	return targets
}

type ProberFunc func(context.Context, ProbeTarget) LatencyResult

func UdpPing(ctx context.Context, target ProbeTarget) LatencyResult {
	pinger, err := probing.NewPinger(target.IP.String())
	if err != nil {
		slog.Error("latency: error creating pinger for device", "device address", target.IP, "interface", target.InterfaceName, "error", err)
		return LatencyResult{Device: target.Device, InterfaceName: target.InterfaceName, IP: target.IP, Reachable: false}
	}

	pinger.Count = 3
	pinger.Timeout = 10 * time.Second
	pinger.Size = 56 // 64 bytes - 8 byte ICMP header
	pinger.SetPrivileged(true)

	if err := pinger.Run(); err != nil {
		slog.Error("latency: error while probing", "address", target.IP, "interface", target.InterfaceName, "error", err)
		return LatencyResult{Device: target.Device, InterfaceName: target.InterfaceName, IP: target.IP, Reachable: false}
	}
	stats := pinger.Statistics()
	results := LatencyResult{
		Device:        target.Device,
		InterfaceName: target.InterfaceName,
		IP:            target.IP,
	}

	results.Reachable = true
	if stats.PacketLoss == 100 {
		results.Reachable = false
	}
	results.Avg = int64(stats.AvgRtt)
	results.Min = int64(stats.MinRtt)
	results.Max = int64(stats.MaxRtt)
	results.Loss = stats.PacketLoss
	return results
}

type SmartContractorFunc func(context.Context, string, string) (*ContractData, error)

type DeviceCache struct {
	Devices []serviceability.Device
	Lock    sync.Mutex
}

type LatencyResult struct {
	Min           int64      `json:"min_latency_ns"`
	Max           int64      `json:"max_latency_ns"`
	Avg           int64      `json:"avg_latency_ns"`
	Loss          float64    `json:"loss_percentage"`
	Device        DeviceInfo `json:"-"`
	InterfaceName string     `json:"interface_name,omitempty"`
	IP            net.IP     `json:"-"` // Probed IP (from interface or PublicIp)
	Reachable     bool       `json:"reachable"`
}

func (l *LatencyResult) MarshalJSON() ([]byte, error) {
	type Alias LatencyResult

	return json.Marshal(&struct {
		DevicePk   string `json:"device_pk"`
		DeviceIP   string `json:"device_ip"`
		DeviceCode string `json:"device_code"`
		*Alias
	}{
		DeviceIP:   l.IP.String(),
		DevicePk:   base58.Encode(l.Device.PubKey[:]),
		DeviceCode: l.Device.Code,
		Alias:      (*Alias)(l),
	})
}

type LatencyResults struct {
	Results []LatencyResult
	Lock    sync.RWMutex `json:"-"`
}

func (l *LatencyResults) MarshalJSON() ([]byte, error) {
	l.Lock.RLock()
	defer l.Lock.RUnlock()
	return json.Marshal(l.Results)
}

type LatencyManager struct {
	SmartContractFunc    SmartContractorFunc
	proberFunc           ProberFunc
	DeviceCache          *DeviceCache
	ResultsCache         *LatencyResults
	programId            string
	rpcEndpoint          string
	probeInterval        time.Duration
	cacheUpdateInterval  time.Duration
	metricsEnabled       bool
	probeTunnelEndpoints bool // when true, also probe UserTunnelEndpoint interfaces
}

type Option func(*LatencyManager)

func NewLatencyManager(options ...Option) *LatencyManager {
	lm := &LatencyManager{
		DeviceCache:  &DeviceCache{Devices: []serviceability.Device{}, Lock: sync.Mutex{}},
		ResultsCache: &LatencyResults{Results: []LatencyResult{}, Lock: sync.RWMutex{}},
		// Set default values
		SmartContractFunc:   FetchContractData,
		proberFunc:          UdpPing,
		probeInterval:       10 * time.Second,
		cacheUpdateInterval: 300 * time.Second,
		metricsEnabled:      false,
	}
	for _, o := range options {
		o(lm)
	}
	return lm
}

func WithSmartContractFunc(f SmartContractorFunc) Option {
	return func(l *LatencyManager) {
		l.SmartContractFunc = f
	}
}

func WithProberFunc(f ProberFunc) Option {
	return func(l *LatencyManager) {
		l.proberFunc = f
	}
}

func WithProgramID(id string) Option {
	return func(l *LatencyManager) {
		l.programId = id
	}
}

func WithRpcEndpoint(endpoint string) Option {
	return func(l *LatencyManager) {
		l.rpcEndpoint = endpoint
	}
}

func WithProbeInterval(interval time.Duration) Option {
	return func(l *LatencyManager) {
		l.probeInterval = interval
	}
}

func WithCacheUpdateInterval(interval time.Duration) Option {
	return func(l *LatencyManager) {
		l.cacheUpdateInterval = interval
	}
}

func WithMetricsEnabled(enabled bool) Option {
	return func(l *LatencyManager) {
		l.metricsEnabled = enabled
	}
}

func WithProbeTunnelEndpoints(enabled bool) Option {
	return func(l *LatencyManager) {
		l.probeTunnelEndpoints = enabled
	}
}

func (l *LatencyManager) Start(ctx context.Context) error {

	// start goroutine for fetching smartcontract devices
	go func() {
		fetch := func() {
			ctx, cancel := context.WithTimeout(ctx, serviceabilityProgramDataFetchTimeout)
			defer cancel()
			contractData, err := l.SmartContractFunc(ctx, l.programId, l.rpcEndpoint)
			if err != nil {
				slog.Error("latency: error fetching smart contract data", "error", err)
				return
			}

			if len(contractData.Devices) == 0 {
				slog.Warn("latency: smartcontract data contained 0 devices")
				return
			}
			slog.Debug("latency: updating cache", "number of devices updated", len(contractData.Devices))
			l.DeviceCache.Lock.Lock()
			l.DeviceCache.Devices = contractData.Devices
			l.DeviceCache.Lock.Unlock()
		}
		// don't wait for first tick and populate cache
		fetch()

		ticker := time.NewTicker(l.cacheUpdateInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fetch()
			}
		}
	}()

	// TODO: insert ticker here to repeat probing on a clock tick
	go func() {
		probe := func() {
			resultsCache := []LatencyResult{}
			wg := sync.WaitGroup{}
			resultsChan := make(chan LatencyResult)

			l.DeviceCache.Lock.Lock()
			devices := l.DeviceCache.Devices
			l.DeviceCache.Lock.Unlock()

			// Build list of all targets across all devices
			var targets []ProbeTarget
			for _, device := range devices {
				targets = append(targets, GetProbeTargets(device, l.probeTunnelEndpoints)...)
			}

			// Probe each target
			for _, target := range targets {
				wg.Go(func() {
					resultsChan <- l.proberFunc(ctx, target)
				})
			}
			go func() {
				wg.Wait()
				close(resultsChan)
			}()

			for result := range resultsChan {
				resultsCache = append(resultsCache, result)

				if l.metricsEnabled {
					devicePk := base58.Encode(result.Device.PubKey[:])
					deviceIp := result.IP

					MetricLatencyRttMin.WithLabelValues(devicePk, result.Device.Code, deviceIp.String()).Set(float64(result.Min))
					MetricLatencyRttAvg.WithLabelValues(devicePk, result.Device.Code, deviceIp.String()).Set(float64(result.Avg))
					MetricLatencyRttMax.WithLabelValues(devicePk, result.Device.Code, deviceIp.String()).Set(float64(result.Max))

					MetricLatencyLoss.WithLabelValues(devicePk, result.Device.Code, deviceIp.String()).Set(result.Loss)

					reachableValue := 0
					if result.Reachable {
						reachableValue = 1
					}
					MetricLatencyReachable.WithLabelValues(devicePk, result.Device.Code, deviceIp.String()).Set(float64(reachableValue))
				}
			}

			l.ResultsCache.Lock.Lock()
			l.ResultsCache.Results = resultsCache
			l.ResultsCache.Lock.Unlock()
		}

		// don't wait for first tick to ping stuff
		probe()

		ticker := time.NewTicker(l.probeInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe()
			}
		}
	}()
	<-ctx.Done()
	slog.Info("latency: closing manager")

	return nil
}

func (l *LatencyManager) ServeLatency(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(l.ResultsCache)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "error generating latency: %v", err)
		return
	}
	_, _ = w.Write(data)
}

func (l *LatencyManager) GetDeviceCache() []serviceability.Device {
	l.DeviceCache.Lock.Lock()
	defer l.DeviceCache.Lock.Unlock()
	devices := make([]serviceability.Device, len(l.DeviceCache.Devices))
	copy(devices, l.DeviceCache.Devices)
	return devices
}

func (l *LatencyManager) GetResultsCache() []LatencyResult {
	l.ResultsCache.Lock.RLock()
	defer l.ResultsCache.Lock.RUnlock()
	results := make([]LatencyResult, len(l.ResultsCache.Results))
	copy(results, l.ResultsCache.Results)
	return results
}
