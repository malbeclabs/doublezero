package latency_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58/base58"
	"golang.org/x/sys/unix"
)

func TestLatencyManager(t *testing.T) {
	tests := []struct {
		Name         string
		Description  string
		DeviceCache  []serviceability.Device
		ResultsCache []latency.LatencyResult
	}{
		{
			Name:        "validate_device_cache",
			Description: "validate the device cache is as we think",
			DeviceCache: []serviceability.Device{
				{
					AccountType: serviceability.DeviceType,
					PublicIp:    [4]uint8{127, 0, 0, 1},
					PubKey:      [32]byte{1},
					Code:        "dev01",
				},
			},
			ResultsCache: []latency.LatencyResult{
				{
					Min:  1,
					Max:  10,
					Avg:  5,
					Loss: 0,
					Device: serviceability.Device{
						AccountType: serviceability.DeviceType,
						PublicIp:    [4]uint8{127, 0, 0, 1},
						PubKey:      [32]byte{1},
						Code:        "dev01",
					},
					Reachable: true,
				},
			},
		},
	}

	sentContractData := make(chan struct{}, 1)
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		sentContractData <- struct{}{}
		return &latency.ContractData{
			Devices: []serviceability.Device{
				{
					AccountType: serviceability.DeviceType,
					PublicIp:    [4]uint8{127, 0, 0, 1},
					PubKey:      [32]byte{1},
					Code:        "dev01",
				},
			},
		}, nil
	}
	sentLatencyData := make(chan struct{}, 1)
	mockProberFunc := func(ctx context.Context, d serviceability.Device) latency.LatencyResult {
		sentLatencyData <- struct{}{}
		return latency.LatencyResult{
			Min:       1,
			Max:       10,
			Avg:       5,
			Loss:      0,
			Device:    d,
			Reachable: true,
		}
	}
	programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID(programId),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: tests[0].DeviceCache, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		if err := manager.Start(ctx); err != nil {
			log.Fatalf("error: %v", err)
		}
	}()
	t.Run("check_device_cache_is_correct", func(t *testing.T) {
		select {
		case <-sentContractData:
			<-time.After(1 * time.Second) // wait for device cache to be populated but this sucks
		case <-time.After(5 * time.Second):
			t.Fatal("timed out while waiting for device cache")
		}
		got := manager.GetDeviceCache()
		log.Printf("got: %+v", got)
		if diff := cmp.Diff(tests[0].DeviceCache, got); diff != "" {
			t.Errorf("DeviceCache mismatch (-want +got): %s\n", diff)
		}
	})

	t.Run("check_results_cache_is_correct", func(t *testing.T) {
		select {
		case <-sentLatencyData:
			<-time.After(1 * time.Second) // wait for latency cache to be populated but this sucks
		case <-time.After(5 * time.Second):
			t.Fatal("timed out while waiting for results cache")
		}

		if diff := cmp.Diff(tests[0].ResultsCache, manager.GetResultsCache()); diff != "" {
			t.Errorf("ResultsCache mismatch (-want +got): %s\n", diff)
		}
	})

	f, err := os.CreateTemp("/tmp", "doublezero.sock")
	if err != nil {
		t.Fatalf("error creating sock file: %v", err)
	}
	defer os.Remove(f.Name())
	_ = unix.Unlink(f.Name())

	lis, err := net.Listen("unix", f.Name())
	if err != nil {
		t.Fatalf("error creating listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /latency", manager.ServeLatency)

	server := http.Server{
		Handler: mux,
	}
	defer server.Close()
	go func() {
		err := server.Serve(lis)
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("error during http serving: %v", err)
		}
	}()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", f.Name())

			},
		},
	}

	t.Run("check_results_via_http_are_correct", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://localhost/latency", nil)
		if err != nil {
			t.Fatalf("error generating http request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error while making http request: %v", err)
		}
		defer resp.Body.Close()

		buf, _ := io.ReadAll(resp.Body)
		got := []map[string]any{}
		if err := json.Unmarshal(buf, &got); err != nil {
			log.Fatalf("error unmarshaling latency data: %v", err)
		}

		want := []map[string]any{
			{
				"device_pk":       base58.Encode(tests[0].DeviceCache[0].PubKey[:]),
				"device_code":     tests[0].DeviceCache[0].Code,
				"device_ip":       "127.0.0.1",
				"min_latency_ns":  float64(1),
				"max_latency_ns":  float64(10),
				"avg_latency_ns":  float64(5),
				"loss_percentage": float64(0),
				"reachable":       true,
			},
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("LatencyResults mismatch (-want +got): %s\n", diff)
		}
	})
}

func TestLatencyUdpPing(t *testing.T) {
	devices := []serviceability.Device{
		{
			AccountType: serviceability.DeviceType,
			PublicIp:    [4]uint8{127, 0, 0, 1},
			PubKey:      [32]byte{1},
			Code:        "dev01",
		},
	}

	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		return &latency.ContractData{Devices: devices}, nil
	}

	resultChan := make(chan struct{})

	mockProber := func(ctx context.Context, d serviceability.Device) latency.LatencyResult {
		result := latency.UdpPing(ctx, d)
		resultChan <- struct{}{}
		return result
	}

	programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProber),
		latency.WithProgramID(programId),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: devices, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := manager.Start(ctx); err != nil {
			log.Fatalf("error: %v", err)
		}
	}()

	select {
	case <-resultChan:
		// Result was sent but buffer for the result cache getting populated; not great
		<-time.After(2 * time.Second)
	case <-time.After(10 * time.Second):
		t.Fatalf("time out waiting for probe results")
	}

	want := []latency.LatencyResult{
		{
			Device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{127, 0, 0, 1},
				PubKey:      [32]byte{1},
				Code:        "dev01",
			},
			Reachable: true,
			Loss:      0,
		},
	}
	if diff := cmp.Diff(
		want,
		manager.GetResultsCache(),
		cmpopts.IgnoreFields(latency.LatencyResults{}, "Lock"),
		cmpopts.IgnoreFields(latency.LatencyResult{}, "Avg", "Max", "Min"),
	); diff != "" {
		t.Errorf("ResultsCache mismatch (-want +got): %s\n", diff)
	}
	results := manager.GetResultsCache()
	Avg := results[0].Avg
	Min := results[0].Min
	Max := results[0].Max
	if Avg == 0 || Min == 0 || Max == 0 {
		log.Fatalf("avg/min/max latency values should be non-zero: %d/%d/%d", Avg, Min, Max)
	}
}

func TestLatencyServerHttp(t *testing.T) {

}
