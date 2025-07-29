package collector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// MockWheresitupCollector for testing
type MockWheresitupCollector struct {
	RunFunc   func(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error
	runCalled bool
	mu        sync.Mutex
}

func (m *MockWheresitupCollector) Run(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error {
	m.mu.Lock()
	m.runCalled = true
	m.mu.Unlock()

	if m.RunFunc != nil {
		return m.RunFunc(ctx, interval, dryRun, jobIDsFile, stateDir, outputDir)
	}
	// Simulate running for a short time
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (m *MockWheresitupCollector) wasRunCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runCalled
}

// MockRipeAtlasCollector for testing
type MockRipeAtlasCollector struct {
	RunFunc   func(ctx context.Context, dryRun bool, probesPerLocation int, stateDir, outputDir string, measurementInterval, exportInterval time.Duration) error
	runCalled bool
	mu        sync.Mutex
}

func (m *MockRipeAtlasCollector) Run(ctx context.Context, dryRun bool, probesPerLocation int, stateDir, outputDir string, measurementInterval, exportInterval time.Duration) error {
	m.mu.Lock()
	m.runCalled = true
	m.mu.Unlock()

	if m.RunFunc != nil {
		return m.RunFunc(ctx, dryRun, probesPerLocation, stateDir, outputDir, measurementInterval, exportInterval)
	}
	// Simulate running for a short time
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (m *MockRipeAtlasCollector) wasRunCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runCalled
}

func TestInternetLatency_Collector_Run(t *testing.T) {
	t.Run("Normal operation and shutdown", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		mockRipe := &MockRipeAtlasCollector{}
		mockWheresitup := &MockWheresitupCollector{}

		config := Config{
			Logger:     log,
			Wheresitup: mockWheresitup,
			RipeAtlas:  mockRipe,

			WheresitupCollectionInterval: 1 * time.Minute,
			RipeAtlasMeasurementInterval: 1 * time.Hour,
			RipeAtlasExportInterval:      2 * time.Minute,
			DryRun:                       true,
			ProcessedJobsFile:            "test.csv",
			StateDir:                     t.TempDir(),
			OutputDir:                    t.TempDir(),
			ProbesPerLocation:            2,
		}

		// Create a context that we can cancel
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Run in a goroutine
		errCh := make(chan error, 1)
		go func() {
			c, err := New(config)
			if err != nil {
				errCh <- err
				return
			}
			err = c.Run(ctx)
			errCh <- err
		}()

		// Let it run for a bit
		time.Sleep(50 * time.Millisecond)

		// Verify collectors were called
		require.True(t, mockWheresitup.wasRunCalled(), "Wheresitup collector should have been called")
		require.True(t, mockRipe.wasRunCalled(), "RIPE Atlas collector should have been called")

		// The function should be running indefinitely
		select {
		case err := <-errCh:
			require.NoError(t, err, "Run() should not return error")
		case <-time.After(100 * time.Millisecond):
			// This is expected - the function runs indefinitely
		}
	})

	t.Run("Wheresitup collector error", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		mockRipe := &MockRipeAtlasCollector{}
		mockWheresitup := &MockWheresitupCollector{
			RunFunc: func(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error {
				return errors.New("wheresitup error")
			},
		}

		config := Config{
			Logger:     log,
			Wheresitup: mockWheresitup,
			RipeAtlas:  mockRipe,

			WheresitupCollectionInterval: 1 * time.Minute,
			RipeAtlasMeasurementInterval: 1 * time.Hour,
			RipeAtlasExportInterval:      2 * time.Minute,
			DryRun:                       true,
			ProcessedJobsFile:            "test.csv",
			StateDir:                     t.TempDir(),
			OutputDir:                    t.TempDir(),
			ProbesPerLocation:            2,
		}

		// Create a context
		ctx := t.Context()

		// Run in a goroutine
		errCh := make(chan error, 1)
		go func() {
			c, err := New(config)
			if err != nil {
				errCh <- err
				return
			}
			err = c.Run(ctx)
			errCh <- err
		}()

		// Wait for error
		select {
		case err := <-errCh:
			require.NotNil(t, err, "Expected error from wheresitup collector")
			require.Equal(t, "failed to run collectors: wheresitup collector error: wheresitup error", err.Error(), "Unexpected error")
		case <-time.After(1 * time.Second):
			t.Fatal("Expected error but got timeout")
		}
	})
}
