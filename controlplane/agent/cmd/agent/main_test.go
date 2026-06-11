package main

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"google.golang.org/grpc"
)

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
			expected: "a418fb48875fda25486c99b94b15adcb2128c7d1f7175008697a0b1dce3b2743",
		},
		{
			name:     "consistent hash for same input",
			input:    "test config",
			expected: "4369f6f9a25e73c79637fa29e84ad7928a251e170d2ef46f636d0a901c0d09bb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := computeChecksum(tt.input)
			hash2 := computeChecksum(tt.input)

			if hash1 != hash2 {
				t.Errorf("hash should be consistent for same input")
			}
			if len(hash1) != 64 {
				t.Errorf("SHA256 hash should be 64 hex characters, got %d", len(hash1))
			}
			if hash1 != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, hash1)
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
			mockClient := &mockControllerClient{
				getConfigFunc: func(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					return tt.mockResponse, nil
				},
			}

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
	ctx := context.Background()
	err := applyConfig(ctx, nil, "", 3600)
	if err != nil {
		t.Errorf("expected no error for empty config, got %v", err)
	}
}

func TestShouldApplyConfig(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := 60 * time.Second

	tests := []struct {
		name        string
		cachedHash  string
		newHash     string
		lastApplied time.Time
		now         time.Time
		want        bool
	}{
		{
			name:        "first run - no cached hash",
			cachedHash:  "",
			newHash:     "abc123",
			lastApplied: time.Time{},
			now:         now,
			want:        true,
		},
		{
			name:        "config changed",
			cachedHash:  "abc123",
			newHash:     "def456",
			lastApplied: now,
			now:         now,
			want:        true,
		},
		{
			name:        "config unchanged within timeout",
			cachedHash:  "abc123",
			newHash:     "abc123",
			lastApplied: now,
			now:         now.Add(30 * time.Second),
			want:        false,
		},
		{
			name:        "config unchanged but timeout exceeded",
			cachedHash:  "abc123",
			newHash:     "abc123",
			lastApplied: now,
			now:         now.Add(61 * time.Second),
			want:        true,
		},
		{
			name:        "config unchanged at exact timeout boundary",
			cachedHash:  "abc123",
			newHash:     "abc123",
			lastApplied: now,
			now:         now.Add(60 * time.Second),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldApplyConfig(tt.cachedHash, tt.newHash, tt.lastApplied, timeout, tt.now)
			if got != tt.want {
				t.Errorf("shouldApplyConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
