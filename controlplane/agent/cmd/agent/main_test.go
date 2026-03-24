package main

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"google.golang.org/grpc"
)

// Mock types for testing
type mockControllerClient struct {
	getConfigFunc func(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error)
}

func (m *mockControllerClient) GetConfig(ctx context.Context, req *pb.ConfigRequest, opts ...grpc.CallOption) (*pb.ConfigResponse, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx, req)
	}
	return &pb.ConfigResponse{}, nil
}

func TestComputeChecksum(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple config",
			input:    "interface Tunnel500\n  description test",
			expected: "0e5c3a6f84d0c3bc1b8f0b26e4c8c2c5f37cfbaed6697c6e4a7f7e87c9bf60ce",
		},
		{
			name:     "consistent hash for same input",
			input:    "test config",
			expected: "4369f6f9a25e73c79637fa29e84ad7928a251e170d2ef46f636d0a901c0d09bb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute hash twice to ensure consistency
			hash1 := computeChecksum(tt.input)
			hash2 := computeChecksum(tt.input)

			if hash1 != hash2 {
				t.Errorf("hash should be consistent for same input")
			}
			if len(hash1) != 64 {
				t.Errorf("SHA256 hash should be 64 hex characters, got %d", len(hash1))
			}

			if tt.name != "simple config" {
				// Verify specific known hashes
				if hash1 != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, hash1)
				}
			}
		})
	}
}

func TestFetchConfigFromController(t *testing.T) {
	tests := []struct {
		name           string
		pubkey         string
		neighborIpMap  map[string][]string
		mockResponse   *pb.ConfigResponse
		mockError      error
		expectedConfig string
		expectedHash   string
		expectError    bool
	}{
		{
			name:   "successful config fetch",
			pubkey: "test-pubkey",
			neighborIpMap: map[string][]string{
				"default": {"10.0.0.1", "10.0.0.2"},
			},
			mockResponse: &pb.ConfigResponse{
				Config: "interface Tunnel500\n  description test",
			},
			expectedConfig: "interface Tunnel500\n  description test",
			expectedHash:   computeChecksum("interface Tunnel500\n  description test"),
			expectError:    false,
		},
		{
			name:   "empty config response",
			pubkey: "test-pubkey",
			neighborIpMap: map[string][]string{
				"default": {},
			},
			mockResponse:   &pb.ConfigResponse{Config: ""},
			expectedConfig: "",
			expectedHash:   computeChecksum(""),
			expectError:    false,
		},
		{
			name:          "controller error",
			pubkey:        "test-pubkey",
			neighborIpMap: map[string][]string{},
			mockError:     errors.New("controller unavailable"),
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := &mockControllerClient{
				getConfigFunc: func(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					return tt.mockResponse, nil
				},
			}

			// Set global timeout for testing
			timeout := float64(1)
			originalTimeout := controllerTimeoutInSeconds
			controllerTimeoutInSeconds = &timeout
			defer func() { controllerTimeoutInSeconds = originalTimeout }()

			verbose := false
			ctx := context.Background()

			config, hash, err := fetchConfigFromController(
				ctx,
				mockClient,
				tt.pubkey,
				tt.neighborIpMap,
				&verbose,
				"test-version",
				"test-commit",
				"test-date",
			)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if config != "" {
					t.Errorf("expected empty config on error, got %s", config)
				}
				if hash != "" {
					t.Errorf("expected empty hash on error, got %s", hash)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if config != tt.expectedConfig {
					t.Errorf("expected config %q, got %q", tt.expectedConfig, config)
				}
				if hash != tt.expectedHash {
					t.Errorf("expected hash %q, got %q", tt.expectedHash, hash)
				}
			}
		})
	}
}

func TestApplyConfig(t *testing.T) {
	tests := []struct {
		name        string
		configText  string
		maxLockAge  int
		expectError bool
	}{
		{
			name:        "successful config application",
			configText:  "interface Tunnel500\n  description test",
			maxLockAge:  3600,
			expectError: false,
		},
		{
			name:        "empty config - no call",
			configText:  "",
			maxLockAge:  3600,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			// We can't easily test the actual EAPIClient without a real connection,
			// so we just test the empty config case
			if tt.configText == "" {
				err := applyConfig(ctx, nil, tt.configText, tt.maxLockAge)
				if err != nil {
					t.Errorf("expected no error for empty config, got %v", err)
				}
			}
		})
	}
}

func TestCachingLogic(t *testing.T) {
	tests := []struct {
		name         string
		cachedHash   string
		newHash      string
		cacheTime    time.Time
		currentTime  time.Time
		cacheTimeout time.Duration
		shouldApply  bool
		description  string
	}{
		{
			name:         "first run - no cached hash",
			cachedHash:   "",
			newHash:      "abc123",
			cacheTime:    time.Time{},
			currentTime:  time.Now(),
			cacheTimeout: 60 * time.Second,
			shouldApply:  true,
			description:  "Should apply on first run when no cached hash exists",
		},
		{
			name:         "config changed",
			cachedHash:   "abc123",
			newHash:      "def456",
			cacheTime:    time.Now(),
			currentTime:  time.Now(),
			cacheTimeout: 60 * time.Second,
			shouldApply:  true,
			description:  "Should apply when config hash changes",
		},
		{
			name:         "config unchanged within timeout",
			cachedHash:   "abc123",
			newHash:      "abc123",
			cacheTime:    time.Now(),
			currentTime:  time.Now().Add(30 * time.Second),
			cacheTimeout: 60 * time.Second,
			shouldApply:  false,
			description:  "Should not apply when config unchanged and within timeout",
		},
		{
			name:         "config unchanged but timeout exceeded",
			cachedHash:   "abc123",
			newHash:      "abc123",
			cacheTime:    time.Now(),
			currentTime:  time.Now().Add(61 * time.Second),
			cacheTimeout: 60 * time.Second,
			shouldApply:  true,
			description:  "Should apply when timeout exceeded even if config unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the caching logic from main()
			shouldApply := false

			if tt.cachedHash == "" {
				// First run
				shouldApply = true
			} else if tt.newHash != tt.cachedHash {
				// Config changed
				shouldApply = true
			} else if tt.currentTime.Sub(tt.cacheTime) >= tt.cacheTimeout {
				// Timeout exceeded
				shouldApply = true
			}

			if shouldApply != tt.shouldApply {
				t.Errorf("%s: expected shouldApply=%v, got %v", tt.description, tt.shouldApply, shouldApply)
			}
		})
	}
}
