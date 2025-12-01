package tpuquic

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTools_Solana_TPUQUICPing_ConfigValidateDefaults(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{}
	require.NoError(t, cfg.Validate())

	require.Equal(t, DefaultCount, cfg.Count)
	require.Equal(t, DefaultInterval, cfg.Interval)
	require.NotNil(t, cfg.Clock)

	// DialConfig defaults
	require.Equal(t, DefaultSrc.String(), cfg.DialConfig.Src)
	require.Equal(t, DefaultKeepAlivePeriod, cfg.DialConfig.KeepAlivePeriod)
}

func TestTools_Solana_TPUQUICPing_ConfigValidateRequiresDst(t *testing.T) {
	t.Parallel()

	// Config.Validate no longer knows about dst, but Ping requires a dst.
	ctx := context.Background()
	log := testLogger()
	cfg := PingConfig{}

	res, err := Ping(ctx, log, "", cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_PingMissingDst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testLogger()
	cfg := PingConfig{} // all defaults

	res, err := Ping(ctx, log, "", cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_PingInvalidDst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testLogger()
	cfg := PingConfig{}

	// Invalid address format -> net.ResolveUDPAddr should fail
	res, err := Ping(ctx, log, "not-a-valid-address", cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_PingInvalidSrc(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testLogger()
	cfg := PingConfig{
		DialConfig: DialConfig{
			Src: "invalid-local", // missing port
		},
	}

	res, err := Ping(ctx, log, "127.0.0.1:1234", cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_DialFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	log := testLogger()

	cfg := PingConfig{
		DialConfig: DialConfig{
			Src: "0.0.0.0:0",
			// Timeouts are passed through to quic-go; we mostly care that
			// a failure is surfaced as an error from Ping.
		},
	}

	// Port 9 is typically discard/no QUIC server; handshake should fail.
	res, err := Ping(ctx, log, "127.0.0.1:9", cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_SuccessAgainstLocalQUICServer(t *testing.T) {
	t.Parallel()

	serverAddrCh := make(chan string, 1)
	serverDone := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(serverDone)

		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = tls.NoClientCert

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
		<-sess.Context().Done()
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	log := testLogger()

	cfg := PingConfig{
		DialConfig: DialConfig{
			Src: "0.0.0.0:0",
		},
		Count:    3,
		Interval: 50 * time.Millisecond,
	}

	res, err := Ping(ctx, log, dst, cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.Success)
	require.Len(t, res.ConnectionStats, cfg.Count)

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server did not exit")
	}

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICPing_FailureAgainstLocalQUICServer(t *testing.T) {
	t.Parallel()

	serverAddrCh := make(chan string, 1)
	serverDone := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(serverDone)

		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = tls.NoClientCert
		tlsConf.NextProtos = []string{"wrong-alpn"}

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

		time.Sleep(750 * time.Millisecond)
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	log := testLogger()

	cfg := PingConfig{
		DialConfig: DialConfig{
			Src: "0.0.0.0:0",
		},
	}

	res, err := Ping(ctx, log, dst, cfg)
	require.Error(t, err)
	require.Nil(t, res)

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server did not exit")
	}

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICPing_CreateTLSConfig(t *testing.T) {
	t.Parallel()
	cfg, err := createTlsConfig()
	require.NoError(t, err)
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	require.Len(t, cfg.Certificates, 1)
	require.Contains(t, cfg.NextProtos, ALPNTPUProtocolID)
	require.NotEmpty(t, cfg.Certificates[0].Certificate)
}

func TestTools_Solana_TPUQUICPing_NewDummyCert(t *testing.T) {
	t.Parallel()
	certPEM, keyPEM := newDummyX509Certificate()
	require.NotEmpty(t, certPEM)
	require.NotEmpty(t, keyPEM)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.Equal(t, DefaultCommonName, cert.Subject.CommonName)
	require.Equal(t, DefaultCommonName, cert.Issuer.CommonName)
	require.Equal(t, 1975, cert.NotBefore.Year())
	require.Equal(t, 4096, cert.NotAfter.Year())

	kb, _ := pem.Decode(keyPEM)
	require.NotNil(t, kb)
	priv, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	require.NoError(t, err)
	_, ok := priv.(ed25519.PrivateKey)
	require.True(t, ok)
}

func TestTools_Solana_TPUQUICPing_SubjectAltNameExt(t *testing.T) {
	t.Parallel()
	ext := createSubjectAltNameExtension()
	require.Equal(t, asn1.ObjectIdentifier{2, 5, 29, 17}, ext.Id)
	require.NotEmpty(t, ext.Value)
}

func TestTools_Solana_TPUQUICPing_SuccessAgainstLocalQUICServer_Interface(t *testing.T) {
	t.Parallel()

	// On non-Linux/Darwin we only have a stub bindToDevice, so skip.
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("interface binding not implemented on this platform")
	}

	ifaceName, _ := getLoopbackInterfaceV4(t)

	serverAddrCh := make(chan string, 1)
	serverDone := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(serverDone)

		tlsConf, err := createTlsConfig()
		if err != nil {
			errCh <- err
			return
		}
		tlsConf.ClientAuth = tls.NoClientCert

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
		<-sess.Context().Done()
	}()

	dst := <-serverAddrCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	log := testLogger()

	cfg := PingConfig{
		DialConfig: DialConfig{
			Interface: ifaceName,
			Src:       "0.0.0.0:0",
		},
		Count:    3,
		Interval: 50 * time.Millisecond,
	}

	res, err := Ping(ctx, log, dst, cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.Success)
	require.Len(t, res.ConnectionStats, cfg.Count)

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server did not exit")
	}

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestTools_Solana_TPUQUICPing_BindToDevice_Interface_NoPanic(t *testing.T) {
	t.Parallel()

	ifaceName, ifaceIP := getLoopbackInterfaceV4(t)

	laddr := &net.UDPAddr{IP: ifaceIP, Port: 0}
	conn, err := net.ListenUDP("udp", laddr)
	require.NoError(t, err)
	defer conn.Close()

	// We deliberately do NOT assert on the error, because:
	// - Linux SO_BINDTODEVICE often requires capabilities and may return EPERM.
	// - Darwin IP_BOUND_IF/IPV6_BOUND_IF should usually work, but we don't
	//   want tests to depend on privileges.
	// - Other platforms use a stub that just returns an error.
	// This is mainly a smoke test + coverage for bindToDevice.
	_ = bindToDevice(conn, ifaceName)
}

func getLoopbackInterfaceV4(t *testing.T) (name string, ip net.IP) {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		require.NoError(t, err)

		for _, a := range addrs {
			var ipNet *net.IPNet
			switch v := a.(type) {
			case *net.IPNet:
				ipNet = v
			case *net.IPAddr:
				ipNet = &net.IPNet{IP: v.IP, Mask: v.IP.DefaultMask()}
			default:
				continue
			}
			if ipNet == nil || ipNet.IP == nil {
				continue
			}
			if v4 := ipNet.IP.To4(); v4 != nil {
				return iface.Name, v4
			}
		}
	}

	t.Skip("no up loopback interface with IPv4 address found")
	return "", nil
}
