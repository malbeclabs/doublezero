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

type ProberFunc func(context.Context, serviceability.Device) LatencyResult

func UdpPing(ctx context.Context, d serviceability.Device) LatencyResult {
	addr := net.IP(d.PublicIp[:])
	pinger, err := probing.NewPinger(addr.String())
	if err != nil {
		slog.Error("latency: error creating pinger for device", "device address", addr, "error", err)
		return LatencyResult{Device: d, Reachable: false}
	}

	pinger.Count = 3
	pinger.Timeout = 10 * time.Second
	pinger.Size = 56 // 64 bytes - 8 byte ICMP header
	pinger.SetPrivileged(true)

	if err := pinger.Run(); err != nil {
		slog.Error("latency: error while probing", "address", addr, "error", err)
		return LatencyResult{Device: d, Reachable: false}
	}
	stats := pinger.Statistics()
	results := LatencyResult{Device: d}

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
		DevicePk   string `json:"device_pk"`
		DeviceIP   string `json:"device_ip"`
		DeviceCode string `json:"device_code"`
		*Alias
	}{
		DeviceIP:   net.IP(l.Device.PublicIp[:]).String(),
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
	SmartContractFunc   SmartContractorFunc
	proberFunc          ProberFunc
	DeviceCache         *DeviceCache
	ResultsCache        *LatencyResults
	programId           string
	rpcEndpoint         string
	probeInterval       time.Duration
	cacheUpdateInterval time.Duration
	metricsEnabled      bool
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

			for _, device := range devices {
				wg.Add(1)
				go func() {
					resultsChan <- l.proberFunc(ctx, device)
					wg.Done()
				}()
			}
			go func() {
				wg.Wait()
				close(resultsChan)
			}()

			for result := range resultsChan {
				resultsCache = append(resultsCache, result)

				if l.metricsEnabled {
					devicePk := base58.Encode(result.Device.PubKey[:])
					deviceIp := net.IP(result.Device.PublicIp[:]).String()

					MetricLatencyRttMin.WithLabelValues(devicePk, result.Device.Code, deviceIp).Set(float64(result.Min))
					MetricLatencyRttAvg.WithLabelValues(devicePk, result.Device.Code, deviceIp).Set(float64(result.Avg))
					MetricLatencyRttMax.WithLabelValues(devicePk, result.Device.Code, deviceIp).Set(float64(result.Max))

					MetricLatencyLoss.WithLabelValues(devicePk, result.Device.Code, deviceIp).Set(result.Loss)

					reachableValue := 0
					if result.Reachable {
						reachableValue = 1
					}
					MetricLatencyReachable.WithLabelValues(devicePk, result.Device.Code, deviceIp).Set(float64(reachableValue))
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
		_, _ = w.Write([]byte(fmt.Sprintf("error generating latency: %v", err)))
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
