package latency

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

func TestClient_Latency(t *testing.T) {
	t.Parallel()

	t.Run("NewManager_defaults", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		ccfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		m, err := NewManager(Config{
			Logger: newTestLogger(t),
			Config: ccfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{}, nil
				}}
			},
		})
		require.NoError(t, err)
		require.Equal(t, defaultProbeInterval, m.cfg.ProbeInterval)
		require.Equal(t, defaultDevicesFetchInterval, m.cfg.DevicesFetchInterval)
		require.Equal(t, defaultFetchTimeout, m.cfg.FetchTimeout)
		require.Equal(t, defaultProbeTimeout, m.cfg.ProbeTimeout)
		require.Equal(t, defaultConcurrency, m.cfg.Concurrency)
		require.NotNil(t, m.cfg.PingFunc)
	})

	t.Run("NewManager_requires_fields", func(t *testing.T) {
		t.Parallel()
		_, err := NewManager(Config{})
		require.Error(t, err)
		_, err = NewManager(Config{Logger: newTestLogger(t)})
		require.Error(t, err)
		_, err = NewManager(Config{Logger: newTestLogger(t), Config: &config.Config{}})
		require.Error(t, err)
	})

	t.Run("probeDevices_and_ServeLatency_JSON", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		ccfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		var pk1, pk2 [32]byte
		copy(pk1[:], bytes.Repeat([]byte{0x11}, 32))
		copy(pk2[:], bytes.Repeat([]byte{0x22}, 32))
		var ip1, ip2 [4]uint8
		copy(ip1[:], net.IPv4(10, 0, 0, 1))
		copy(ip2[:], net.IPv4(10, 0, 0, 2))
		devs := []serviceability.Device{
			{Code: "dev-A", PubKey: pk1, PublicIp: ip1},
			{Code: "dev-B", PubKey: pk2, PublicIp: ip2},
		}

		mock := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Devices: devs}, nil
			},
		}

		var pingMu sync.Mutex
		var pings int
		ping := func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
			pingMu.Lock()
			pings++
			pingMu.Unlock()
			return LatencyResult{Min: 1, Max: 3, Avg: 2, Loss: 0, Reachable: true, Device: d}
		}

		m, err := NewManager(Config{
			Logger:                      newTestLogger(t),
			Config:                      ccfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return mock },
			PingFunc:                    ping,
			ProbeTimeout:                500 * time.Millisecond,
			FetchTimeout:                500 * time.Millisecond,
			Concurrency:                 8,
		})
		require.NoError(t, err)

		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/latency", nil)
		m.ServeLatency(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var got []map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got, 2)

		require.Equal(t, "dev-A", got[0]["device_code"])
		require.Equal(t, mustPKFromBytes(pk1), got[0]["device_pk"])
		require.Equal(t, net.IP(ip1[:]).String(), got[0]["device_ip"])
		require.EqualValues(t, 1, got[0]["min_latency_ns"])
		require.EqualValues(t, 3, got[0]["max_latency_ns"])
		require.EqualValues(t, 2, got[0]["avg_latency_ns"])
		require.EqualValues(t, true, got[0]["reachable"])

		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodHead, "/latency", nil)
		m.ServeLatency(rec2, req2)
		require.Equal(t, http.StatusOK, rec2.Code)

		rec3 := httptest.NewRecorder()
		req3 := httptest.NewRequest(http.MethodPost, "/latency", nil)
		m.ServeLatency(rec3, req3)
		require.Equal(t, http.StatusMethodNotAllowed, rec3.Code)

		pingMu.Lock()
		gotPings := pings
		pingMu.Unlock()
		require.Equal(t, len(devs), gotPings)
	})

	t.Run("Start_initial_probe_and_config_change_reprobe", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		var pk [32]byte
		copy(pk[:], bytes.Repeat([]byte{0x42}, 32))
		var ip [4]uint8
		copy(ip[:], net.IPv4(192, 0, 2, 10))

		first := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "one", PubKey: pk, PublicIp: ip}}}, nil
		}}
		second := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "two", PubKey: pk, PublicIp: ip}}}, nil
		}}

		var mu sync.Mutex
		call := 0
		factory := func(string, solana.PublicKey) ServiceabilityClient {
			mu.Lock()
			defer mu.Unlock()
			call++
			if call == 1 {
				return first
			}
			return second
		}

		var pingCountMu sync.Mutex
		var pingCount int
		ping := func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
			pingCountMu.Lock()
			pingCount++
			pingCountMu.Unlock()
			return LatencyResult{Min: 5, Max: 5, Avg: 5, Reachable: true, Device: d}
		}

		m, err := NewManager(Config{
			Logger:                      newTestLogger(t),
			Config:                      cfg,
			NewServiceabilityClientFunc: factory,
			PingFunc:                    ping,
			ProbeInterval:               30 * time.Millisecond,
			DevicesFetchInterval:        30 * time.Millisecond,
			FetchTimeout:                200 * time.Millisecond,
			ProbeTimeout:                200 * time.Millisecond,
			Concurrency:                 4,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- m.Start(ctx) }()

		require.Eventually(t, func() bool {
			pingCountMu.Lock()
			c := pingCount
			pingCountMu.Unlock()
			return c >= 1
		}, 2*time.Second, 10*time.Millisecond)

		_, err = cfg.Update("http://ledger2", someProgramID())
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/latency", nil)
			m.ServeLatency(rec, req)
			if rec.Code != http.StatusOK {
				return false
			}
			var got []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				return false
			}
			for _, r := range got {
				if r["device_code"] == "two" {
					return true
				}
			}
			return false
		}, 2*time.Second, 10*time.Millisecond)

		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatalf("Start did not exit after cancel")
		}
	})

	t.Run("ServeLatency_when_no_results_emits_empty_array", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)
		m, err := NewManager(Config{
			Logger: newTestLogger(t),
			Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{}, nil
				}}
			},
			PingFunc: func(context.Context, *slog.Logger, serviceability.Device) LatencyResult { return LatencyResult{} },
		})
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/latency", nil)
		m.ServeLatency(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "[]\n", rec.Body.String())
	})

	t.Run("refreshDevices_without_client_noop", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		// Manager with a factory we won't call
		m, err := NewManager(Config{
			Logger:                      newTestLogger(t),
			Config:                      cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return &mockServiceabilityClient{} },
		})
		require.NoError(t, err)

		// serviceability is nil until recreateServiceabilityClient
		require.NotNil(t, m)
		m.refreshDevices(context.Background()) // should not panic
		// results/devices remain nil
		m.mu.RLock()
		defer m.mu.RUnlock()
		require.Nil(t, m.serviceability)
		require.Nil(t, m.devices)
	})

	t.Run("Load_malformed_program_id_errors", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "bad.json")
		// invalid base58 for a PublicKey -> config.Load should fail
		require.NoError(t, os.WriteFile(p, []byte(`{
		  "ledger_rpc_url": "http://ledger",
		  "serviceability_program_id": "not-base58"
		}`), 0o644))
		_, err := config.Load(p)
		require.Error(t, err)
	})

	t.Run("probeDevices_recovers_from_ping_panic", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		var pk [32]byte
		copy(pk[:], bytes.Repeat([]byte{0xEE}, 32))
		var ip [4]uint8
		copy(ip[:], net.IPv4(203, 0, 113, 5))
		devs := []serviceability.Device{{Code: "ok", PubKey: pk, PublicIp: ip}, {Code: "boom", PubKey: pk, PublicIp: ip}}

		mock := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: devs}, nil
		}}

		ping := func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
			if d.Code == "boom" {
				panic("kaboom")
			}
			return LatencyResult{Min: 7, Max: 7, Avg: 7, Reachable: true, Device: d}
		}

		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return mock },
			PingFunc:                    ping, Concurrency: 2,
		})
		require.NoError(t, err)

		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		// both entries present; one with default zero-values due to panic
		m.mu.RLock()
		results := append([]LatencyResult(nil), m.results...)
		m.mu.RUnlock()
		require.Len(t, results, 2)

		var ok, boom *LatencyResult
		if results[0].Device.Code == "ok" {
			ok, boom = &results[0], &results[1]
		} else {
			ok, boom = &results[1], &results[0]
		}
		require.EqualValues(t, 7, ok.Avg)
		require.True(t, ok.Reachable)
		// panic case: zero-value (Avg 0, Reachable false) unless your PingFunc sets otherwise post-recover
		require.EqualValues(t, 0, boom.Avg)
		require.False(t, boom.Reachable)
	})

	t.Run("recreateServiceabilityClient_clears_devices_and_results", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		mock := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "pre"}}}, nil
		}}
		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return mock },
			PingFunc: func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
				return LatencyResult{Avg: 1, Device: d}
			},
		})
		require.NoError(t, err)

		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		m.mu.RLock()
		require.NotEmpty(t, m.devices)
		require.NotEmpty(t, m.results)
		m.mu.RUnlock()

		m.recreateServiceabilityClient()
		m.mu.RLock()
		require.Empty(t, m.devices)
		require.Empty(t, m.results)
		m.mu.RUnlock()
	})

	t.Run("probeDevices_caps_concurrency", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		// many devices to stress pool
		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 9))
		N := 32
		devs := make([]serviceability.Device, N)
		for i := 0; i < N; i++ {
			devs[i] = serviceability.Device{Code: fmt.Sprintf("d%02d", i), PubKey: pk, PublicIp: ip}
		}

		mock := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: devs}, nil
		}}

		var cur, max int64
		ping := func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
			n := atomic.AddInt64(&cur, 1)
			atomic.StoreInt64(&max, maxInt(atomic.LoadInt64(&max), n))
			time.Sleep(20 * time.Millisecond) // keep overlap
			atomic.AddInt64(&cur, -1)
			return LatencyResult{Device: d}
		}

		const workers = 5
		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return mock },
			PingFunc:                    ping, Concurrency: workers,
		})
		require.NoError(t, err)

		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		require.LessOrEqual(t, int(atomic.LoadInt64(&max)), workers)
	})

	t.Run("probeDevices_respects_ctx_cancel", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		// enough devices that we'd see lots of pings if not cancelled
		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 8))
		devs := make([]serviceability.Device, 50)
		for i := range devs {
			devs[i] = serviceability.Device{Code: fmt.Sprintf("d%d", i), PubKey: pk, PublicIp: ip}
		}

		mock := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: devs}, nil
		}}

		var pings int64
		ping := func(ctx context.Context, l *slog.Logger, d serviceability.Device) LatencyResult {
			atomic.AddInt64(&pings, 1)
			select {
			case <-ctx.Done(): // simulates long work but stops on cancel
				return LatencyResult{}
			case <-time.After(200 * time.Millisecond):
				return LatencyResult{Device: d}
			}
		}

		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return mock },
			PingFunc:                    ping, Concurrency: 8, ProbeTimeout: 2 * time.Second,
		})
		require.NoError(t, err)

		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())

		ctx, cancel := context.WithCancel(context.Background())
		go m.probeDevices(ctx)
		time.Sleep(30 * time.Millisecond) // let some workers start
		cancel()

		// Ensure not all devices were pinged; cancellation cut it short
		time.Sleep(50 * time.Millisecond)
		require.Less(t, int(atomic.LoadInt64(&pings)), len(devs))
	})

	t.Run("refreshDevices_times_out_and_preserves_old_devices", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		// first client returns one device
		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 7))
		first := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "keep", PubKey: pk, PublicIp: ip}}}, nil
		}}
		// second client blocks until ctx timeout
		blocker := &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}

		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return first },
			FetchTimeout:                50 * time.Millisecond,
		})
		require.NoError(t, err)
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())

		// swap in blocking client
		m.mu.Lock()
		m.serviceability = blocker
		m.mu.Unlock()
		m.refreshDevices(context.Background())

		// devices unchanged
		m.mu.RLock()
		defer m.mu.RUnlock()
		require.Len(t, m.devices, 1)
		require.Equal(t, "keep", m.devices[0].Code)
	})

	t.Run("ServeLatency_HEAD_empty_body_and_headers", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)

		m, err := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{}, nil
				}}
			},
		})
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/latency", nil)
		m.ServeLatency(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, rec.Body.Bytes())
	})

	t.Run("probeDevices_enforces_Ping_timeout", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, _ := config.Load(cfgPath)

		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 4))
		devs := []serviceability.Device{{Code: "slow", PubKey: pk, PublicIp: ip}}

		m, _ := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{Devices: devs}, nil
				}}
			},
			PingFunc: func(ctx context.Context, _ *slog.Logger, d serviceability.Device) LatencyResult {
				<-ctx.Done()
				return LatencyResult{Device: d} // times out
			},
			ProbeTimeout: 50 * time.Millisecond,
		})
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())

		start := time.Now()
		m.probeDevices(context.Background())
		elapsed := time.Since(start)
		require.Less(t, elapsed, 500*time.Millisecond) // bounded by timeout, not hanging
	})

	t.Run("probeDevices_preserves_device_order", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, _ := config.Load(cfgPath)

		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 5))
		devs := []serviceability.Device{
			{Code: "a", PubKey: pk, PublicIp: ip},
			{Code: "b", PubKey: pk, PublicIp: ip},
			{Code: "c", PubKey: pk, PublicIp: ip},
		}
		m, _ := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{Devices: devs}, nil
				}}
			},
			PingFunc: func(ctx context.Context, _ *slog.Logger, d serviceability.Device) LatencyResult {
				if d.Code == "b" {
					time.Sleep(20 * time.Millisecond)
				} // reordering pressure
				return LatencyResult{Device: d, Avg: 1}
			},
			Concurrency: 3,
		})
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		m.mu.RLock()
		got := append([]LatencyResult(nil), m.results...)
		m.mu.RUnlock()
		require.Equal(t, "a", got[0].Device.Code)
		require.Equal(t, "b", got[1].Device.Code)
		require.Equal(t, "c", got[2].Device.Code)
	})

	t.Run("ServeLatency_sets_headers_on_GET_with_results", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		cfg, _ := config.Load(cfgPath)

		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 6))
		devs := []serviceability.Device{{Code: "x", PubKey: pk, PublicIp: ip}}
		m, _ := NewManager(Config{
			Logger: newTestLogger(t), Config: cfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient {
				return &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{Devices: devs}, nil
				}}
			},
			PingFunc: func(context.Context, *slog.Logger, serviceability.Device) LatencyResult {
				return LatencyResult{Avg: 1, Device: devs[0]}
			},
		})
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())
		m.probeDevices(context.Background())

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/latency", nil)
		m.ServeLatency(rec, req)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("refreshDevices_error_preserves_old_devices", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		ccfg, _ := config.Load(cfgPath)

		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 7))
		ok := &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "keep", PubKey: pk, PublicIp: ip}}}, nil
		}}
		bad := &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return nil, fmt.Errorf("boom")
		}}

		m, _ := NewManager(Config{
			Logger: newTestLogger(t), Config: ccfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return ok },
		})
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())

		m.mu.Lock()
		m.serviceability = bad
		m.mu.Unlock()
		m.refreshDevices(context.Background())

		m.mu.RLock()
		defer m.mu.RUnlock()
		require.Len(t, m.devices, 1)
		require.Equal(t, "keep", m.devices[0].Code)
	})

	t.Run("refreshDevices_error_preserves_old_devices", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeTempConfigFile(t, "http://ledger", someProgramID())
		ccfg, _ := config.Load(cfgPath)

		var pk [32]byte
		var ip [4]uint8
		copy(ip[:], net.IPv4(10, 0, 0, 7))
		ok := &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{{Code: "keep", PubKey: pk, PublicIp: ip}}}, nil
		}}
		bad := &mockServiceabilityClient{GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
			return nil, fmt.Errorf("boom")
		}}

		m, _ := NewManager(Config{
			Logger: newTestLogger(t), Config: ccfg,
			NewServiceabilityClientFunc: func(string, solana.PublicKey) ServiceabilityClient { return ok },
		})
		m.recreateServiceabilityClient()
		m.refreshDevices(context.Background())

		m.mu.Lock()
		m.serviceability = bad
		m.mu.Unlock()
		m.refreshDevices(context.Background())

		m.mu.RLock()
		defer m.mu.RUnlock()
		require.Len(t, m.devices, 1)
		require.Equal(t, "keep", m.devices[0].Code)
	})

}

type mockServiceabilityClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("test", t.Name())
	return log
}

func writeTempConfigFile(t *testing.T, ledgerURL string, pid solana.PublicKey) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	b, err := json.Marshal(map[string]any{
		"ledger_rpc_url":            ledgerURL,
		"serviceability_program_id": pid.String(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, b, 0o644))
	return p
}

func mustPKFromBytes(b [32]byte) string {
	return solana.PublicKeyFromBytes(b[:]).String()
}

func someProgramID() solana.PublicKey {
	return solana.NewWallet().PublicKey()
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
