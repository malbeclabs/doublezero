package state

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type MockStateIngestClient struct {
	UploadSnapshotFunc func(ctx context.Context, kind string, timestamp time.Time, data []byte) (string, error)
}

func (m *MockStateIngestClient) UploadSnapshot(ctx context.Context, kind string, timestamp time.Time, data []byte) (string, error) {
	if m.UploadSnapshotFunc == nil {
		return "", nil
	}
	return m.UploadSnapshotFunc(ctx, kind, timestamp, data)
}

type MockEapiMgrServiceClient struct {
	RunShowCmdFunc    func(ctx context.Context, in *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error)
	RunConfigCmdsFunc func(ctx context.Context, in *aristapb.RunConfigCmdsRequest, opts ...grpc.CallOption) (*aristapb.RunConfigCmdsResponse, error)
}

func (m *MockEapiMgrServiceClient) RunShowCmd(ctx context.Context, in *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
	if m.RunShowCmdFunc == nil {
		return nil, nil
	}
	return m.RunShowCmdFunc(ctx, in, opts...)
}

func (m *MockEapiMgrServiceClient) RunConfigCmds(ctx context.Context, in *aristapb.RunConfigCmdsRequest, opts ...grpc.CallOption) (*aristapb.RunConfigCmdsResponse, error) {
	if m.RunConfigCmdsFunc == nil {
		return nil, nil
	}
	return m.RunConfigCmdsFunc(ctx, in, opts...)
}

func TestTelemetry_StateCollector_ConfigValidate_RequiredFields(t *testing.T) {
	t.Parallel()

	t.Run("missing logger", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.Logger = nil
		require.ErrorContains(t, cfg.Validate(), "logger is required")
	})

	t.Run("missing eapi", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = nil
		require.ErrorContains(t, cfg.Validate(), "eapi is required")
	})

	t.Run("missing ingest", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.StateIngest = nil
		require.ErrorContains(t, cfg.Validate(), "ingest is required")
	})

	t.Run("interval <= 0", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.Interval = 0
		require.ErrorContains(t, cfg.Validate(), "interval must be greater than 0")
	})

	t.Run("device pk is required", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.DevicePK = solana.PublicKey{}
		require.ErrorContains(t, cfg.Validate(), "device pk is required")
	})
}

func TestTelemetry_StateCollector_ConfigValidate_Defaults(t *testing.T) {
	t.Parallel()

	t.Run("sets real clock when nil", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.Clock = nil
		require.NoError(t, cfg.Validate())
		require.NotNil(t, cfg.Clock)
		_ = cfg.Clock.Now()
	})

	t.Run("defaults concurrency when <= 0", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.Concurrency = 0
		require.NoError(t, cfg.Validate())
		require.Equal(t, defaultConcurrency, cfg.Concurrency)
	})

	t.Run("defaults commands when empty", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.Commands = nil
		require.NoError(t, cfg.Validate())
		require.Equal(t, defaultCommands, cfg.Commands)
	})
}

func TestTelemetry_StateCollector_SanitizeCommandAsKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"show snmp mib ifmib ifindex", "snmp-mib-ifmib-ifindex"},
		{"show isis database detail", "isis-database-detail"},
		{"SHOW isis database detail", "show-isis-database-detail"}, // case-sensitive prefix
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sanitizeCommandAsKind(tc.in))
		})
	}
}

func TestTelemetry_StateCollector_CollectStateSnapshot_Success_UploadPayload(t *testing.T) {
	t.Parallel()

	fc := clockwork.NewFakeClockAt(time.Date(2025, 12, 18, 15, 4, 5, 0, time.UTC))
	cfg := newValidCollectorCfg(t)
	cfg.Clock = fc
	cfg.Commands = []string{"show snmp mib ifmib ifindex"}

	command := cfg.Commands[0]
	wantKind := "snmp-mib-ifmib-ifindex"
	wantNow := fc.Now().UTC()

	cfg.EAPI = &MockEapiMgrServiceClient{
		RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
			require.Equal(t, command, in.Command)
			return &aristapb.RunShowCmdResponse{
				Response: &aristapb.EapiResponse{
					Success:   true,
					Responses: []string{`{"ok":true,"value":123}`},
				},
			}, nil
		},
	}

	var gotKind string
	var gotTS time.Time
	var gotData []byte

	cfg.StateIngest = &MockStateIngestClient{
		UploadSnapshotFunc: func(ctx context.Context, kind string, ts time.Time, data []byte) (string, error) {
			gotKind = kind
			gotTS = ts
			gotData = append([]byte(nil), data...)
			return "id-123", nil
		},
	}

	col, err := NewCollector(cfg)
	require.NoError(t, err)

	require.NoError(t, col.collectStateSnapshot(context.Background(), command))

	require.Equal(t, wantKind, gotKind)
	require.True(t, gotTS.Equal(wantNow))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotData, &payload))

	metadata, ok := payload["metadata"].(map[string]any)
	require.True(t, ok)

	require.Equal(t, wantKind, metadata["kind"])
	require.Equal(t, cfg.DevicePK.String(), metadata["device"])
	require.Equal(t, wantNow.Format(time.RFC3339), metadata["timestamp"])

	dataObj, ok := payload["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, dataObj["ok"])
	require.Equal(t, float64(123), dataObj["value"])
}

func TestTelemetry_StateCollector_CollectStateSnapshot_ErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("eapi call error", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = &MockEapiMgrServiceClient{
			RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				return nil, errors.New("boom")
			},
		}
		col, err := NewCollector(cfg)
		require.NoError(t, err)
		require.ErrorContains(t, col.collectStateSnapshot(context.Background(), "show x"), "failed to execute command")
	})

	t.Run("nil response wrapper", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = &MockEapiMgrServiceClient{
			RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				return &aristapb.RunShowCmdResponse{Response: nil}, nil
			},
		}
		col, err := NewCollector(cfg)
		require.NoError(t, err)
		require.ErrorContains(t, col.collectStateSnapshot(context.Background(), "show x"), "no response from arista eapi")
	})

	t.Run("response success=false", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = &MockEapiMgrServiceClient{
			RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{
						Success:      false,
						ErrorCode:    42,
						ErrorMessage: "nope",
					},
				}, nil
			},
		}
		col, err := NewCollector(cfg)
		require.NoError(t, err)
		err = col.collectStateSnapshot(context.Background(), "show x")
		require.ErrorContains(t, err, "error from arista eapi")
		require.ErrorContains(t, err, "code=42")
	})

	t.Run("empty responses list", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = &MockEapiMgrServiceClient{
			RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{
						Success:   true,
						Responses: []string{},
					},
				}, nil
			},
		}
		col, err := NewCollector(cfg)
		require.NoError(t, err)
		require.ErrorContains(t, col.collectStateSnapshot(context.Background(), "show x"), "no responses from arista eapi")
	})

	t.Run("upload error wraps command", func(t *testing.T) {
		t.Parallel()
		cfg := newValidCollectorCfg(t)
		cfg.EAPI = &MockEapiMgrServiceClient{
			RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
				return &aristapb.RunShowCmdResponse{
					Response: &aristapb.EapiResponse{
						Success:   true,
						Responses: []string{`{"ok":true}`},
					},
				}, nil
			},
		}
		cfg.StateIngest = &MockStateIngestClient{
			UploadSnapshotFunc: func(ctx context.Context, kind string, ts time.Time, data []byte) (string, error) {
				return "", errors.New("upload failed")
			},
		}
		col, err := NewCollector(cfg)
		require.NoError(t, err)
		cmd := "show upload test"
		err = col.collectStateSnapshot(context.Background(), cmd)
		require.ErrorContains(t, err, "failed to upload state snapshot")
		require.ErrorContains(t, err, cmd)
	})
}

func TestTelemetry_StateCollector_Tick_CollectsAllCommandsOnce(t *testing.T) {
	t.Parallel()

	fc := clockwork.NewFakeClockAt(time.Date(2025, 12, 18, 0, 0, 0, 0, time.UTC))
	cfg := newValidCollectorCfg(t)
	cfg.Clock = fc
	cfg.Commands = []string{
		"show a b",
		"show c d",
		"show e f",
	}
	cfg.Concurrency = 2

	var runShowCalls atomic.Int64
	var uploadCalls atomic.Int64

	cfg.EAPI = &MockEapiMgrServiceClient{
		RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
			runShowCalls.Add(1)
			return &aristapb.RunShowCmdResponse{
				Response: &aristapb.EapiResponse{
					Success:   true,
					Responses: []string{`{"ok":true}`},
				},
			}, nil
		},
	}

	cfg.StateIngest = &MockStateIngestClient{
		UploadSnapshotFunc: func(ctx context.Context, kind string, ts time.Time, data []byte) (string, error) {
			uploadCalls.Add(1)
			return "id", nil
		},
	}

	col, err := NewCollector(cfg)
	require.NoError(t, err)

	require.NoError(t, col.tick(context.Background()))
	require.Equal(t, int64(len(cfg.Commands)), runShowCalls.Load())
	require.Equal(t, int64(len(cfg.Commands)), uploadCalls.Load())
}

func TestTelemetry_StateCollector_Run_InitialTickAndTickerTicks(t *testing.T) {
	t.Parallel()

	fc := clockwork.NewFakeClockAt(time.Date(2025, 12, 18, 0, 0, 0, 0, time.UTC))
	cfg := newValidCollectorCfg(t)
	cfg.Clock = fc
	cfg.Interval = 10 * time.Second
	cfg.Commands = []string{"show a", "show b"}
	cfg.Concurrency = 4

	var uploads atomic.Int64
	initialDone := make(chan struct{})
	var initialOnce sync.Once

	cfg.EAPI = &MockEapiMgrServiceClient{
		RunShowCmdFunc: func(ctx context.Context, in *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
			return &aristapb.RunShowCmdResponse{
				Response: &aristapb.EapiResponse{
					Success:   true,
					Responses: []string{`{"ok":true}`},
				},
			}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	wantTicks := int64(3) // 1 immediate + 2 ticker
	wantUploads := wantTicks * int64(len(cfg.Commands))
	initialUploads := int64(len(cfg.Commands))

	cfg.StateIngest = &MockStateIngestClient{
		UploadSnapshotFunc: func(ctx context.Context, kind string, ts time.Time, data []byte) (string, error) {
			n := uploads.Add(1)
			if n >= initialUploads {
				initialOnce.Do(func() { close(initialDone) })
			}
			if n >= wantUploads {
				cancel()
			}
			return "id", nil
		},
	}

	col, err := NewCollector(cfg)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- col.Run(ctx) }()

	select {
	case <-initialDone:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for initial tick uploads")
	}

	blockCtx, blockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer blockCancel()
	require.NoError(t, fc.BlockUntilContext(blockCtx, 1))

	fc.Advance(cfg.Interval)
	fc.Advance(cfg.Interval)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for Run() to exit")
	}

	require.Equal(t, wantUploads, uploads.Load())
}

func newValidCollectorCfg(t *testing.T) *CollectorConfig {
	t.Helper()
	return &CollectorConfig{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		Clock:       clockwork.NewFakeClock(),
		EAPI:        &MockEapiMgrServiceClient{},
		StateIngest: &MockStateIngestClient{},
		Interval:    1 * time.Second,
		DevicePK:    solana.NewWallet().PrivateKey.PublicKey(),
		Commands:    []string{"show foo bar"},
		Concurrency: 2,
	}
}
