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
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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

func TestTelemetry_StateIngest_Server_Start_MultiListener_CancelStopsAllAndClosesErrCh(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln1.Close() })

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln2.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := s.Start(ctx, cancel, ln1, ln2)

	// Ensure both listeners are serving.
	for _, addr := range []string{ln1.Addr().String(), ln2.Addr().String()} {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		require.NoError(t, err)
		_ = c.Close()
	}

	cancel()

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestTelemetry_StateIngest_Server_Start_MultiListener_OneErrorCancelsOther(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	good, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = good.Close() })

	wantErr := errors.New("accept boom")
	bad := &errListener{err: wantErr}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := s.Start(ctx, cancel, good, bad)

	// Expect the genuine serve error from the failed listener.
	var gotErr error
	select {
	case gotErr = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error after serve failure")
	}
	require.ErrorIs(t, gotErr, wantErr)

	// ctx should be canceled, which stops the other listener too.
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected ctx to be canceled after listener error")
	}

	// errCh must close; no further errors from the clean listener.
	for err := range errCh {
		require.ErrorIs(t, err, wantErr)
	}
}

func TestTelemetry_StateIngest_Server_Start_ServeErrorPropagatesAndCancels(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	require.NoError(t, cfg.Validate())

	s, err := New(slog.Default(), cfg)
	require.NoError(t, err)

	wantErr := errors.New("accept boom")
	ln := &errListener{err: wantErr}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := s.Start(ctx, cancel, ln)

	var gotErr error
	select {
	case gotErr = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start() to forward a serve error")
	}

	require.ErrorIs(t, gotErr, wantErr)

	// ctx should be canceled by the server goroutines' deferred cancel().
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected ctx to be canceled after serve error")
	}

	// errCh must eventually close.
	for range errCh {
	}
}

// TestTelemetry_StateIngest_Server_Start_ListenerCloseIsBenign verifies that a
// listener closed out from under Serve during shutdown (which surfaces as
// net.ErrClosed) is treated as a clean stop: ctx is canceled for teardown, but
// no error is propagated or logged.
func TestTelemetry_StateIngest_Server_Start_ListenerCloseIsBenign(t *testing.T) {
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

	// Close the listener out from under Serve.
	require.NoError(t, ln.Close())

	// Teardown still happens via the goroutine's deferred cancel().
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected ctx to be canceled after listener close")
	}

	// But the close is benign: no error is forwarded.
	for err := range errCh {
		require.NoError(t, err)
	}
}

// errListener is a net.Listener whose Accept always fails with a permanent
// (non-net.ErrClosed) error, used to simulate an unexpected Serve failure.
type errListener struct {
	err error
}

func (l *errListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *errListener) Close() error              { return nil }
func (l *errListener) Addr() net.Addr            { return &net.TCPAddr{} }
