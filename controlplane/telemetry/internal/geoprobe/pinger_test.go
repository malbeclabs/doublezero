package geoprobe

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPinger(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)

	require.NotNil(t, pinger)
	assert.NotNil(t, pinger.log)
	assert.NotNil(t, pinger.cfg)
	assert.NotNil(t, pinger.senders)
	assert.Empty(t, pinger.senders)
}

func TestPinger_AddProbe(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr := ProbeAddress{
		Host: "127.0.0.1",
		Port: 12345,
	}

	err := pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	_, exists := pinger.senders[addr.String()]
	pinger.sendersMu.Unlock()

	assert.True(t, exists, "probe should exist after AddProbe")
}

func TestPinger_AddProbe_Duplicate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr := ProbeAddress{
		Host: "127.0.0.1",
		Port: 12346,
	}

	err := pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	firstSender := pinger.senders[addr.String()].sender
	pinger.sendersMu.Unlock()

	err = pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	secondSender := pinger.senders[addr.String()].sender
	count := len(pinger.senders)
	pinger.sendersMu.Unlock()

	assert.Equal(t, 1, count, "should only have one sender after duplicate AddProbe")
	assert.Equal(t, firstSender, secondSender, "should reuse existing sender on duplicate")
}

func TestPinger_AddProbe_InvalidHost(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr := ProbeAddress{
		Host: "not-an-ip",
		Port: 12347,
	}

	err := pinger.AddProbe(ctx, addr)
	assert.Error(t, err, "should fail with invalid IP address")

	pinger.sendersMu.Lock()
	count := len(pinger.senders)
	pinger.sendersMu.Unlock()

	assert.Equal(t, 0, count, "should not add sender for invalid IP")
}

func TestPinger_RemoveProbe(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr := ProbeAddress{
		Host: "127.0.0.1",
		Port: 12348,
	}

	err := pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	err = pinger.RemoveProbe(addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	_, exists := pinger.senders[addr.String()]
	pinger.sendersMu.Unlock()

	assert.False(t, exists, "probe should not exist after RemoveProbe")
}

func TestPinger_RemoveProbe_NotFound(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)

	addr := ProbeAddress{
		Host: "192.0.2.1",
		Port: 12345,
	}

	err := pinger.RemoveProbe(addr)
	assert.NoError(t, err, "RemoveProbe on non-existent probe should not error")
}

func TestPinger_MeasureAll_Empty(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	results, err := pinger.MeasureAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, results, "MeasureAll with no probes should return empty map")
}

func TestPinger_MeasureAll_WithContext(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        10 * time.Second,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)

	addr := ProbeAddress{
		Host: "127.0.0.1",
		Port: 12349,
	}
	err := pinger.AddProbe(context.Background(), addr)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := pinger.MeasureAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, results, "MeasureAll with cancelled context should return empty results")
}

func TestPinger_Close(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr1 := ProbeAddress{Host: "127.0.0.1", Port: 12352}
	addr2 := ProbeAddress{Host: "127.0.0.1", Port: 12353}

	err := pinger.AddProbe(ctx, addr1)
	require.NoError(t, err)
	err = pinger.AddProbe(ctx, addr2)
	require.NoError(t, err)

	err = pinger.Close()
	assert.NoError(t, err)

	pinger.sendersMu.Lock()
	count := len(pinger.senders)
	pinger.sendersMu.Unlock()

	assert.Equal(t, 0, count, "all senders should be removed after Close")
}

func TestPinger_Concurrent(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			addr := ProbeAddress{
				Host: "127.0.0.1",
				Port: uint16(13000 + id),
			}

			err := pinger.AddProbe(ctx, addr)
			assert.NoError(t, err)

			_, err = pinger.MeasureAll(ctx)
			assert.NoError(t, err)

			err = pinger.RemoveProbe(addr)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	pinger.sendersMu.Lock()
	count := len(pinger.senders)
	pinger.sendersMu.Unlock()

	assert.Equal(t, 0, count, "all probes should be removed after concurrent operations")
}

func TestPinger_AddRemoveSequential(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr := ProbeAddress{
		Host: "127.0.0.1",
		Port: 12354,
	}

	err := pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	count1 := len(pinger.senders)
	pinger.sendersMu.Unlock()
	assert.Equal(t, 1, count1)

	err = pinger.RemoveProbe(addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	count2 := len(pinger.senders)
	pinger.sendersMu.Unlock()
	assert.Equal(t, 0, count2)

	err = pinger.AddProbe(ctx, addr)
	require.NoError(t, err)

	pinger.sendersMu.Lock()
	count3 := len(pinger.senders)
	pinger.sendersMu.Unlock()
	assert.Equal(t, 1, count3)
}

func TestPinger_MeasureAll_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        200 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
		StaggerDelay:        10 * time.Millisecond,
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	numProbes := 2000
	for i := 0; i < numProbes; i++ {
		addr := ProbeAddress{
			Host: "127.0.0.1",
			Port: uint16(15000 + i),
		}
		err := pinger.AddProbe(ctx, addr)
		require.NoError(t, err)
	}

	startTime := time.Now()
	results, err := pinger.MeasureAll(ctx)
	duration := time.Since(startTime)

	require.NoError(t, err)

	t.Logf("Measured %d probes in %v, got %d results", numProbes, duration, len(results))

	assert.Less(t, duration, 5*time.Minute, "measurement should complete in reasonable time with worker pool")
}

func TestPinger_MeasureAll_AllFailed(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	addr1 := ProbeAddress{Host: "192.0.2.254", Port: 12345}
	addr2 := ProbeAddress{Host: "192.0.2.253", Port: 12346}
	addr3 := ProbeAddress{Host: "192.0.2.252", Port: 12347}

	err := pinger.AddProbe(ctx, addr1)
	require.NoError(t, err)
	err = pinger.AddProbe(ctx, addr2)
	require.NoError(t, err)
	err = pinger.AddProbe(ctx, addr3)
	require.NoError(t, err)

	results, err := pinger.MeasureAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, results, "MeasureAll should return empty results when all probes fail")
}

func TestPinger_MeasureAll_LargeScale(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        200 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
		StaggerDelay:        1 * time.Millisecond,
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	numProbes := 5000
	t.Logf("Testing with %d probes (validates worker pool batching)", numProbes)

	for i := 0; i < numProbes; i++ {
		addr := ProbeAddress{
			Host: "127.0.0.1",
			Port: uint16(20000 + i),
		}
		err := pinger.AddProbe(ctx, addr)
		require.NoError(t, err)
	}

	startTime := time.Now()
	results, err := pinger.MeasureAll(ctx)
	duration := time.Since(startTime)

	require.NoError(t, err)
	t.Logf("Completed %d probes in %v, got %d results", numProbes, duration, len(results))
	assert.Less(t, duration, 5*time.Minute, "should complete large batch in reasonable time")
}

func TestPinger_MeasureAll_Staggering(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	staggerDelay := 50 * time.Millisecond
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        100 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
		StaggerDelay:        staggerDelay,
	}

	pinger := NewPinger(cfg)
	ctx := context.Background()

	numProbes := 10
	for i := 0; i < numProbes; i++ {
		addr := ProbeAddress{
			Host: "127.0.0.1",
			Port: uint16(25000 + i),
		}
		err := pinger.AddProbe(ctx, addr)
		require.NoError(t, err)
	}

	startTime := time.Now()
	_, err := pinger.MeasureAll(ctx)
	duration := time.Since(startTime)

	require.NoError(t, err)

	expectedMinDuration := staggerDelay * time.Duration(numProbes-1)
	t.Logf("Duration: %v, Expected min: %v", duration, expectedMinDuration)

	assert.GreaterOrEqual(t, duration, expectedMinDuration,
		"probes should be staggered with delays between them")
}

func TestPinger_MeasureAll_ContextCancellation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &PingerConfig{
		Logger:              logger,
		ProbeTimeout:        200 * time.Millisecond,
		Interval:            1 * time.Second,
		ManagementNamespace: "",
		StaggerDelay:        100 * time.Millisecond,
	}

	pinger := NewPinger(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	numProbes := 100
	for i := 0; i < numProbes; i++ {
		addr := ProbeAddress{
			Host: "127.0.0.1",
			Port: uint16(30000 + i),
		}
		err := pinger.AddProbe(ctx, addr)
		require.NoError(t, err)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	startTime := time.Now()
	results, err := pinger.MeasureAll(ctx)
	duration := time.Since(startTime)

	require.NoError(t, err)
	t.Logf("Context cancelled after %v, got %d partial results out of %d probes",
		duration, len(results), numProbes)

	assert.Less(t, duration, 2*time.Second,
		"should stop promptly after context cancellation")
	assert.Less(t, len(results), numProbes,
		"should return partial results when context is cancelled")
}
