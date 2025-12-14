package tpuquic

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestTools_Solana_TPUQUICConn_DialConfig_Validate_Defaults(t *testing.T) {
	t.Parallel()

	var cfg DialConfig
	require.NoError(t, cfg.Validate())

	require.Equal(t, DefaultSrc.String(), cfg.Src)
	require.Zero(t, cfg.KeepAlivePeriod)
}

func TestTools_Solana_TPUQUICConn_Dial_InvalidDst(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, err := Dial(ctx, "not-a-valid-address", nil)
	require.Error(t, err)
	require.Nil(t, conn)
}

func TestTools_Solana_TPUQUICConn_Dial_InvalidSrc(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cfg := &DialConfig{
		Src: "invalid-local", // missing port -> net.ResolveUDPAddr should fail
	}

	conn, err := Dial(ctx, "127.0.0.1:1234", cfg)
	require.Error(t, err)
	require.Nil(t, conn)
}

func TestTools_Solana_TPUQUICConn_Dial_NilConfig_UsesDefaults(t *testing.T) {
	t.Parallel()

	serverAddrCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = 0

		quicConf := &quic.Config{
			MaxIdleTimeout:  time.Second,
			KeepAlivePeriod: 100 * time.Millisecond,
		}

		l, err := quic.ListenAddr("127.0.0.1:0", tlsConf, quicConf)
		if err != nil {
			errCh <- err
			return
		}
		defer l.Close()

		serverAddrCh <- l.Addr().String()

		// Accept a single connection and then close it; don't wait on Context().
		sess, err := l.Accept(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		_ = sess.CloseWithError(0, "server done")
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := Dial(ctx, dst, nil) // nil cfg -> uses defaults
	require.NoError(t, err)
	require.NotNil(t, conn)

	// We should be using a UDP local addr and the remote addr should match dst.
	_, ok := conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok, "LocalAddr should be *net.UDPAddr")
	require.Equal(t, dst, conn.RemoteAddr().String())

	// ConnectionStats should be callable without panic.
	_ = conn.ConnectionStats()

	require.False(t, conn.IsClosed())

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return conn.IsClosed() }, time.Second, 10*time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICConn_Dial_WithExplicitConfig_Success(t *testing.T) {
	t.Parallel()

	serverAddrCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = 0

		quicConf := &quic.Config{
			MaxIdleTimeout:  time.Second,
			KeepAlivePeriod: 100 * time.Millisecond,
		}

		l, err := quic.ListenAddr("127.0.0.1:0", tlsConf, quicConf)
		if err != nil {
			errCh <- err
			return
		}
		defer l.Close()

		serverAddrCh <- l.Addr().String()

		sess, err := l.Accept(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		_ = sess.CloseWithError(0, "server done")
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := &DialConfig{
		Src:                  "0.0.0.0:0",
		MaxIdleTimeout:       2 * time.Second,
		HandshakeIdleTimeout: time.Second,
		KeepAlivePeriod:      200 * time.Millisecond,
	}

	conn, err := Dial(ctx, dst, cfg)
	require.NoError(t, err)
	require.NotNil(t, conn)

	stats := conn.ConnectionStats()
	_ = stats

	require.False(t, conn.IsClosed())
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return conn.IsClosed() }, time.Second, 10*time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICConn_DialWithRetry_InvalidDst(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, err := DialWithRetry(ctx, "not-a-valid-address", nil, nil)
	require.Error(t, err)
	require.Nil(t, conn)
}

func TestTools_Solana_TPUQUICConn_DialWithRetry_InvalidSrc(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cfg := &DialConfig{
		Src: "invalid-local", // missing port -> net.ResolveUDPAddr should fail
	}

	conn, err := DialWithRetry(ctx, "127.0.0.1:1234", cfg, nil)
	require.Error(t, err)
	require.Nil(t, conn)
}

func TestTools_Solana_TPUQUICConn_DialWithRetry_Success(t *testing.T) {
	t.Parallel()

	serverAddrCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = 0

		quicConf := &quic.Config{
			MaxIdleTimeout:  time.Second,
			KeepAlivePeriod: 100 * time.Millisecond,
		}

		l, err := quic.ListenAddr("127.0.0.1:0", tlsConf, quicConf)
		if err != nil {
			errCh <- err
			return
		}
		defer l.Close()

		serverAddrCh <- l.Addr().String()

		sess, err := l.Accept(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		_ = sess.CloseWithError(0, "server done")
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := &DialConfig{
		Src:                  "0.0.0.0:0",
		MaxIdleTimeout:       2 * time.Second,
		HandshakeIdleTimeout: time.Second,
		KeepAlivePeriod:      200 * time.Millisecond,
	}

	conn, err := DialWithRetry(ctx, dst, cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, conn)

	// Ensure stats and basic methods work.
	_ = conn.ConnectionStats()
	require.False(t, conn.IsClosed())

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return conn.IsClosed() }, time.Second, 10*time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICConn_DialWithRetry_RespectsContextDeadline(t *testing.T) {
	t.Parallel()

	// Very short deadline so we don't sit in backoff long.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	conn, err := DialWithRetry(ctx, "127.0.0.1:65534", nil, nil) // unlikely to have a listener
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, conn)

	// Should return promptly once context is done, not hang for a long time.
	require.Less(t, elapsed, 500*time.Millisecond)
}
