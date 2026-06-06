package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// newTestLogger returns a slog.Logger that discards output, so tests do not
// pollute stdout/stderr.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeListener is a minimal net.Listener used to assert the success path
// without actually binding a socket.
type fakeListener struct{}

func (fakeListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (fakeListener) Close() error              { return nil }
func (fakeListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4zero, Port: 0} }

// wrapEADDRNOTAVAIL builds an error that errors.Is detects as EADDRNOTAVAIL,
// matching how net.Listen surfaces bind failures in production.
func wrapEADDRNOTAVAIL() error {
	return &net.OpError{Op: "listen", Err: &os.SyscallError{Syscall: "bind", Err: syscall.EADDRNOTAVAIL}}
}

func wrapEADDRINUSE() error {
	return &net.OpError{Op: "listen", Err: &os.SyscallError{Syscall: "bind", Err: syscall.EADDRINUSE}}
}

func TestListenWithRetry_SucceedsAfterTransientEADDRNOTAVAIL(t *testing.T) {
	var attempts int32
	listen := func() (net.Listener, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return nil, wrapEADDRNOTAVAIL()
		}
		return fakeListener{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, err := listenWithRetry(ctx, newTestLogger(), listen)
	if err != nil {
		t.Fatalf("expected success after transient failures, got %v", err)
	}
	if listener == nil {
		t.Fatal("expected non-nil listener")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestListenWithRetry_RetriesEADDRINUSE(t *testing.T) {
	var attempts int32
	listen := func() (net.Listener, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			return nil, wrapEADDRINUSE()
		}
		return fakeListener{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := listenWithRetry(ctx, newTestLogger(), listen); err != nil {
		t.Fatalf("expected success after EADDRINUSE retry, got %v", err)
	}
}

func TestListenWithRetry_SurfaceNonRetryableErrorImmediately(t *testing.T) {
	want := errors.New("nope: config wrong")
	var attempts int32
	listen := func() (net.Listener, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, want
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, err := listenWithRetry(ctx, newTestLogger(), listen)
	if !errors.Is(err, want) {
		t.Fatalf("expected non-retryable error to be returned as-is, got %v", err)
	}
	if listener != nil {
		t.Fatal("expected nil listener on error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", got)
	}
}

func TestListenWithRetry_ReturnsOnContextCancel(t *testing.T) {
	listen := func() (net.Listener, error) {
		return nil, wrapEADDRNOTAVAIL()
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the loop is mid-backoff.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	listener, err := listenWithRetry(ctx, newTestLogger(), listen)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if listener != nil {
		t.Fatal("expected nil listener on cancel")
	}
	// The initial backoff is 1s but the cancel must short-circuit it; bound the
	// elapsed time to well under the initial backoff to confirm the select on
	// ctx.Done() is reached.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected prompt return on context cancel, took %v", elapsed)
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		current time.Duration
		want    time.Duration
	}{
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{16 * time.Second, metricsListenerMaxBackoff}, // 32s -> clamped to 30s
		{metricsListenerMaxBackoff, metricsListenerMaxBackoff},
		{2 * metricsListenerMaxBackoff, metricsListenerMaxBackoff}, // never exceeds cap
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.current); got != tc.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", tc.current, got, tc.want)
		}
	}
}

func TestIsRetryableBindError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"EADDRNOTAVAIL", wrapEADDRNOTAVAIL(), true},
		{"EADDRINUSE", wrapEADDRINUSE(), true},
		{"generic", errors.New("nope"), false},
		{"ECONNREFUSED", &os.SyscallError{Syscall: "bind", Err: syscall.ECONNREFUSED}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableBindError(tc.err); got != tc.want {
				t.Fatalf("isRetryableBindError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
