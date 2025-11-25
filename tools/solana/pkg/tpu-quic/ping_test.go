package tpuquic

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestTools_Solana_TPUQUICPing_ConfigValidateDefaults(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{Dst: "127.0.0.1:1234"}
	require.NoError(t, cfg.Validate())
	require.NotNil(t, cfg.Context)
	require.NotNil(t, cfg.Logger)
	require.Equal(t, DefaultDuration, cfg.Duration)
	require.Equal(t, DefaultInterval, cfg.Interval)
	require.Equal(t, DefaultTimeout, cfg.Timeout)
	require.Equal(t, DefaultSrc.String(), cfg.Src)
	require.Equal(t, "127.0.0.1:1234", cfg.Dst)
}

func TestTools_Solana_TPUQUICPing_ConfigValidateRequiresDst(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{}
	require.Error(t, cfg.Validate())
}

func TestTools_Solana_TPUQUICPing_PingMissingDst(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{Quiet: true}
	res, err := Ping(cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_PingInvalidDst(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{Dst: "not-a-valid-address", Quiet: true}
	res, err := Ping(cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_PingInvalidSrc(t *testing.T) {
	t.Parallel()
	cfg := PingConfig{Dst: "127.0.0.1:1234", Src: "invalid-local", Quiet: true}
	res, err := Ping(cfg)
	require.Error(t, err)
	require.Nil(t, res)
}

func TestTools_Solana_TPUQUICPing_DialFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	cfg := PingConfig{
		Context: ctx,
		Dst:     "127.0.0.1:9",
		Src:     "0.0.0.0:0",
		Timeout: 50 * time.Millisecond,
		Quiet:   true,
	}

	res, err := Ping(cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.Success)
	require.Error(t, res.Error)
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
	statsCh := make(chan *quic.ConnectionStats, 10)

	cfg := PingConfig{
		Dst:       dst,
		Src:       net.JoinHostPort("0.0.0.0", "0"),
		Duration:  250 * time.Millisecond,
		Interval:  50 * time.Millisecond,
		Timeout:   500 * time.Millisecond,
		StatsChan: statsCh,
		Quiet:     true,
	}

	res, err := Ping(cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.Success)
	require.NotEmpty(t, res.ConnectionStats)
	require.NotEmpty(t, statsCh)

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

	cfg := PingConfig{
		Dst:     dst,
		Src:     "0.0.0.0:0",
		Timeout: 200 * time.Millisecond,
		Quiet:   true,
	}

	res, err := Ping(cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.Success)
	require.Error(t, res.Error)

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
