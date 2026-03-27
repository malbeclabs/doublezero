package enricher

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIfNameAnnotator_Annotate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Create annotator and manually populate its cache (bypassing Init/DB)
	annotator := &IfNameAnnotator{
		name:   "ifname annotator",
		logger: logger,
		cache: map[string]string{
			"10.0.0.1:100": "eth0",
			"10.0.0.1:200": "eth1",
			"10.0.0.2:300": "ge-0/0/0",
		},
	}

	tests := []struct {
		name                    string
		flow                    *FlowSample
		expectedInputInterface  string
		expectedOutputInterface string
		expectError             bool
	}{
		{
			name: "annotates both input and output interfaces",
			flow: &FlowSample{
				SamplerAddress: net.ParseIP("10.0.0.1"),
				InputIfIndex:   100,
				OutputIfIndex:  200,
			},
			expectedInputInterface:  "eth0",
			expectedOutputInterface: "eth1",
			expectError:             false,
		},
		{
			name: "annotates only input interface when output not found",
			flow: &FlowSample{
				SamplerAddress: net.ParseIP("10.0.0.1"),
				InputIfIndex:   100,
				OutputIfIndex:  999, // Not in cache
			},
			expectedInputInterface:  "eth0",
			expectedOutputInterface: "",
			expectError:             false,
		},
		{
			name: "annotates using different sampler address",
			flow: &FlowSample{
				SamplerAddress: net.ParseIP("10.0.0.2"),
				InputIfIndex:   300,
				OutputIfIndex:  0,
			},
			expectedInputInterface:  "ge-0/0/0",
			expectedOutputInterface: "",
			expectError:             false,
		},
		{
			name: "returns empty strings when sampler address not found",
			flow: &FlowSample{
				SamplerAddress: net.ParseIP("10.0.0.99"),
				InputIfIndex:   100,
				OutputIfIndex:  200,
			},
			expectedInputInterface:  "",
			expectedOutputInterface: "",
			expectError:             false,
		},
		{
			name: "returns error when sampler address is nil",
			flow: &FlowSample{
				SamplerAddress: nil,
				InputIfIndex:   100,
				OutputIfIndex:  200,
			},
			expectError: true,
		},
		{
			name: "skips annotation when ifindex is zero",
			flow: &FlowSample{
				SamplerAddress: net.ParseIP("10.0.0.1"),
				InputIfIndex:   0,
				OutputIfIndex:  0,
			},
			expectedInputInterface:  "",
			expectedOutputInterface: "",
			expectError:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := annotator.Annotate(tt.flow)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedInputInterface, tt.flow.InputInterface)
			assert.Equal(t, tt.expectedOutputInterface, tt.flow.OutputInterface)
		})
	}
}

func TestIfNameAnnotator_Init_NilQuerier(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	annotator := NewIfNameAnnotator(nil, logger)
	err := annotator.Init(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "querier is required")
}

func TestIfNameAnnotator_String(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	annotator := NewIfNameAnnotator(nil, logger)
	assert.Equal(t, "ifname annotator", annotator.String())
}

func TestIfNameAnnotator_ConcurrentAccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Create annotator with pre-populated cache
	annotator := &IfNameAnnotator{
		name:   "ifname annotator",
		logger: logger,
		cache: map[string]string{
			"10.0.0.1:100": "eth0",
		},
	}

	// Run concurrent Annotate calls
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				flow := &FlowSample{
					SamplerAddress: net.ParseIP("10.0.0.1"),
					InputIfIndex:   100,
					OutputIfIndex:  100,
				}
				err := annotator.Annotate(flow)
				assert.NoError(t, err)
				assert.Equal(t, "eth0", flow.InputInterface)
			}
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for concurrent access test")
	}
}

func TestIfNameAnnotator_ConcurrentReadWrite(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Create annotator with pre-populated cache
	annotator := &IfNameAnnotator{
		name:   "ifname annotator",
		logger: logger,
		cache: map[string]string{
			"10.0.0.1:100": "eth0",
		},
	}

	// Simulate concurrent reads and cache updates
	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				flow := &FlowSample{
					SamplerAddress: net.ParseIP("10.0.0.1"),
					InputIfIndex:   100,
					OutputIfIndex:  0,
				}
				_ = annotator.Annotate(flow)
			}
		}()
	}

	// Writers (simulating cache updates)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				annotator.mu.Lock()
				annotator.cache = map[string]string{
					"10.0.0.1:100": "eth0",
					"10.0.0.1:200": "eth1",
				}
				annotator.mu.Unlock()
			}
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race condition
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for concurrent read/write test")
	}
}

func TestNewIfNameAnnotator(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	annotator := NewIfNameAnnotator(nil, logger)

	assert.NotNil(t, annotator)
	assert.Equal(t, "ifname annotator", annotator.name)
	assert.NotNil(t, annotator.cache)
	assert.Equal(t, logger, annotator.logger)
	assert.Nil(t, annotator.querier)
}
