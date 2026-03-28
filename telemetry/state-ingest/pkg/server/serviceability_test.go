package server

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

type mockServiceabilityRPC struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (f mockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return f.GetProgramDataFunc(ctx)
}

func newPK(t *testing.T) (pk32 [32]byte, pkB58 string) {
	t.Helper()
	_, err := rand.Read(pk32[:])
	require.NoError(t, err)
	return pk32, base58.Encode(pk32[:])
}

func TestTelemetry_StateIngest_ServiceabilityView_Ready_DefaultFalse(t *testing.T) {
	t.Parallel()

	v := NewServiceabilityView(slog.Default(), clockwork.NewFakeClock(), time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	})
	require.False(t, v.Ready())
}

func TestTelemetry_StateIngest_ServiceabilityView_Refresh_PopulatesDevicesAndSetsReady(t *testing.T) {
	t.Parallel()

	pk1, key1 := newPK(t)
	pk2, key2 := newPK(t)

	var d1, d2 serviceability.Device
	copy(d1.PubKey[:], pk1[:])
	copy(d2.PubKey[:], pk2[:])

	v := NewServiceabilityView(slog.Default(), clockwork.NewFakeClock(), time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{d1, d2}}, nil
		},
	})

	require.False(t, v.Ready())
	require.NoError(t, v.Refresh(context.Background()))
	require.True(t, v.Ready())

	_, ok := v.GetDevice(key1)
	require.True(t, ok)
	_, ok = v.GetDevice(key2)
	require.True(t, ok)

	_, ok = v.GetDevice("does-not-exist")
	require.False(t, ok)
}

func TestTelemetry_StateIngest_ServiceabilityView_Refresh_ErrorDoesNotSetReadyOrOverwriteDevices(t *testing.T) {
	t.Parallel()

	pk, key := newPK(t)
	var d serviceability.Device
	copy(d.PubKey[:], pk[:])

	call := 0
	v := NewServiceabilityView(slog.Default(), clockwork.NewFakeClock(), time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			call++
			if call == 1 {
				return &serviceability.ProgramData{Devices: []serviceability.Device{d}}, nil
			}
			return nil, errors.New("boom")
		},
	})

	require.NoError(t, v.Refresh(context.Background()))
	require.True(t, v.Ready())
	_, ok := v.GetDevice(key)
	require.True(t, ok)

	err := v.Refresh(context.Background())
	require.Error(t, err)

	require.True(t, v.Ready(), "ready should remain true after a later refresh failure")
	_, ok = v.GetDevice(key)
	require.True(t, ok, "devices should not be overwritten on refresh failure")
}

func TestTelemetry_StateIngest_ServiceabilityView_Run_InitialRefreshFailureDoesNotSetReadyUntilSuccess(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	interval := 10 * time.Second

	pk, key := newPK(t)
	var d serviceability.Device
	copy(d.PubKey[:], pk[:])

	calls := make(chan int, 10)
	call := 0

	v := NewServiceabilityView(slog.Default(), clk, interval, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			call++
			select {
			case calls <- call:
			default:
			}
			if call == 1 {
				return nil, errors.New("initial fail")
			}
			return &serviceability.ProgramData{Devices: []serviceability.Device{d}}, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- v.Run(ctx) }()

	// Run() calls Refresh() immediately; first call fails.
	select {
	case n := <-calls:
		require.Equal(t, 1, n)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial refresh call")
	}
	require.False(t, v.Ready(), "ready must remain false after initial refresh failure")

	// Ensure Run() has installed the ticker and is waiting on it, then deliver one tick.
	blockCtx, blockCancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(blockCancel)
	require.NoError(t, clk.BlockUntilContext(blockCtx, 1))

	clk.Advance(interval + time.Nanosecond)

	// Second refresh should happen.
	select {
	case n := <-calls:
		require.Equal(t, 2, n)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second refresh call")
	}

	// The RPC returning doesn't guarantee Refresh() has finished swapping the map + setting ready yet.
	require.Eventually(t, func() bool { return v.Ready() }, time.Second, 5*time.Millisecond,
		"ready should become true after a successful refresh")

	require.Eventually(t, func() bool {
		_, ok := v.GetDevice(key)
		return ok
	}, time.Second, 5*time.Millisecond, "device should be present after successful refresh")

	cancel()
	require.NoError(t, <-done)
}

func TestTelemetry_StateIngest_ServiceabilityView_Run_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClock()
	v := NewServiceabilityView(slog.Default(), clk, time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx) }()

	cancel()
	require.NoError(t, <-done)
}
