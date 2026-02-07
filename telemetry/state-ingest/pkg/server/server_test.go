package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Clock: clockwork.NewFakeClock(),
		Presign: mockPresignClient{
			PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
				return nil, errors.New("not used")
			},
		},
		BucketName: "bucket-1",
		ServiceabilityRPC: mockServiceabilityRPC{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{}, nil
			},
		},
		ServiceabilityRefreshInterval: 10 * time.Second,
		ShutdownTimeout:               250 * time.Millisecond,
	}
}

func TestTelemetry_StateIngest_Server_New_NilLogger(t *testing.T) {
	t.Parallel()

	_, err := New(nil, newTestConfig(t))
	require.ErrorContains(t, err, "logger is required")
}

func TestTelemetry_StateIngest_Server_New_InvalidConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{} // missing required fields
	_, err := New(slog.Default(), cfg)
	require.Error(t, err)
}

func TestTelemetry_StateIngest_Server_Serve_ContextCancelStopsServer(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	// Ensure the Serve goroutine has actually started and is accepting.
	c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)
	_ = c.Close()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err) // http.ErrServerClosed should be translated to nil
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

func TestTelemetry_StateIngest_Server_Start_CancelStopsAllAndClosesErrCh(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := s.Start(ctx, cancel, ln)

	// Ensure Serve is running.
	c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)
	_ = c.Close()

	// Cancel everything.
	cancel()

	// Drain errCh until closed; expect no errors for a clean shutdown.
	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestTelemetry_StateIngest_Server_Start_ServeErrorPropagatesAndCancels(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := s.Start(ctx, cancel, ln)

	// Force Serve() to return an error.
	require.NoError(t, ln.Close())

	var gotErr error
	select {
	case gotErr = <-errCh:
		// Might be nil if Serve returns nil (unlikely here), but in practice closing
		// the listener should produce a non-nil error and be forwarded.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start() to forward a serve error")
	}

	require.Error(t, gotErr)

	// ctx should be canceled by the server goroutines' deferred cancel().
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected ctx to be canceled after serve error")
	}

	// errCh must eventually close.
	select {
	case _, ok := <-errCh:
		if ok {
			for range errCh {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected errCh to close")
	}
}
