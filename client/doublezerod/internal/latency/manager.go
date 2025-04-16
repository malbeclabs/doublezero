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

	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/mr-tron/base58"
	probing "github.com/prometheus-community/pro-bing"
)

type ProberFunc func(context.Context, dzsdk.Device) LatencyResult

func UdpPing(ctx context.Context, d dzsdk.Device) LatencyResult {
	addr := net.IP(d.PublicIp[:])
	pinger, err := probing.NewPinger(addr.String())
	if err != nil {
		slog.Error("latency: error creating pinger for device", "device address", addr, "error", err)
		return LatencyResult{Device: d, Reachable: false}
	}

	pinger.Count = 3
	pinger.Timeout = 10 * time.Second
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
	Devices []dzsdk.Device
	Lock    sync.Mutex
}

type LatencyResult struct {
	Min       int64        `json:"min_latency_ns"`
	Max       int64        `json:"max_latency_ns"`
	Avg       int64        `json:"avg_latency_ns"`
	Loss      float64      `json:"loss_percentage"`
	Device    dzsdk.Device `json:"-"`
	Reachable bool         `json:"reachable"`
}

func (l *LatencyResult) MarshalJSON() ([]byte, error) {
	type Alias LatencyResult
	return json.Marshal(&struct {
		DevicePk string `json:"device_pk"`
		DeviceIP string `json:"device_ip"`
		*Alias
	}{
		DeviceIP: net.IP(l.Device.PublicIp[:]).String(),
		DevicePk: base58.Encode(l.Device.PubKey[:]),
		Alias:    (*Alias)(l),
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
	SmartContractFunc SmartContractorFunc
	ProberFunc        ProberFunc
	DeviceCache       *DeviceCache
	ResultsCache      *LatencyResults
}

func NewLatencyManager(s SmartContractorFunc, p ProberFunc) *LatencyManager {
	return &LatencyManager{
		SmartContractFunc: s,
		ProberFunc:        p,
		DeviceCache:       &DeviceCache{Devices: []dzsdk.Device{}, Lock: sync.Mutex{}},
		ResultsCache:      &LatencyResults{Results: []LatencyResult{}, Lock: sync.RWMutex{}},
	}
}

func (l *LatencyManager) Start(ctx context.Context, programId string, rpcEndpoint string) error {

	// start goroutine for fetching smartcontract devices
	go func() {
		fetch := func() {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			contractData, err := l.SmartContractFunc(ctx, programId, rpcEndpoint)
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

		ticker := time.NewTicker(30 * time.Second)
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
					resultsChan <- l.ProberFunc(ctx, device)
					wg.Done()
				}()
			}
			go func() {
				wg.Wait()
				close(resultsChan)
			}()

			for result := range resultsChan {
				resultsCache = append(resultsCache, result)
			}

			l.ResultsCache.Lock.Lock()
			l.ResultsCache.Results = resultsCache
			l.ResultsCache.Lock.Unlock()
		}

		// don't wait for first tick to ping stuff
		probe()

		ticker := time.NewTicker(30 * time.Second)
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

func (l *LatencyManager) GetDeviceCache() []dzsdk.Device {
	l.DeviceCache.Lock.Lock()
	defer l.DeviceCache.Lock.Unlock()
	devices := make([]dzsdk.Device, len(l.DeviceCache.Devices))
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
