package tpuquic

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

const (
	ALPNTPUProtocolID = "solana-tpu"
	DefaultCommonName = "Solana node"
)

var (
	DefaultSrc = net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}
)

type DialConfig struct {
	Interface string
	Src       string

	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	KeepAlivePeriod      time.Duration
}

func (cfg *DialConfig) Validate() error {
	if cfg.Src == "" {
		cfg.Src = DefaultSrc.String()
	}
	return nil
}

type Conn struct {
	srcConn  *net.UDPConn
	dialConn *quic.Conn
}

func Dial(ctx context.Context, dst string, cfg *DialConfig) (*Conn, error) {
	if cfg == nil {
		cfg = &DialConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate dial config: %w", err)
	}

	// Resolve destination UDP address
	dstAddr, err := net.ResolveUDPAddr("udp", dst)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote addr %s: %w", dst, err)
	}

	// Resolve source UDP address
	srcAddr, err := net.ResolveUDPAddr("udp", cfg.Src)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve local addr %s: %w", cfg.Src, err)
	}

	// Bind our own UDP socket
	srcConn, err := net.ListenUDP("udp", srcAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", srcAddr, err)
	}

	// Bind to interface if specified
	if cfg.Interface != "" {
		if err := bindToDevice(srcConn, cfg.Interface); err != nil {
			return nil, fmt.Errorf("failed to bind to device %s: %w", cfg.Interface, err)
		}
	}

	// Create TLS config
	tlsConfig, err := createTlsConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config: %w", err)
	}

	// Dial QUIC session
	conn, err := quic.Dial(ctx, srcConn, dstAddr, tlsConfig, &quic.Config{
		MaxIdleTimeout:       cfg.MaxIdleTimeout,
		HandshakeIdleTimeout: cfg.HandshakeIdleTimeout,
		KeepAlivePeriod:      cfg.KeepAlivePeriod,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s from %s: %w", dstAddr, srcAddr, err)
	}

	return &Conn{
		dialConn: conn,
		srcConn:  srcConn,
	}, nil
}

func (c *Conn) ConnectionStats() quic.ConnectionStats {
	return c.dialConn.ConnectionStats()
}

func (c *Conn) IsClosed() bool {
	select {
	case <-c.dialConn.Context().Done():
		return true
	default:
		return false
	}
}

func (c *Conn) Context() context.Context {
	return c.dialConn.Context()
}

func (c *Conn) LocalAddr() net.Addr {
	return c.dialConn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.dialConn.RemoteAddr()
}

func (c *Conn) Close() error {
	if c.dialConn == nil {
		if c.srcConn != nil {
			return c.srcConn.Close()
		}
		return nil
	}
	err := c.dialConn.CloseWithError(0, "client done")
	if c.srcConn != nil {
		srcErr := c.srcConn.Close()
		if srcErr != nil {
			err = errors.Join(err, srcErr)
		}
	}
	return err
}

func createTlsConfig() (*tls.Config, error) {
	certPem, privPem := newDummyX509Certificate()
	cert, err := tls.X509KeyPair(certPem, privPem)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{ALPNTPUProtocolID},
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
	}, nil
}

func newDummyX509Certificate() ([]byte, []byte) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, nil
	}

	tmpl := x509.Certificate{
		Version:            3,
		SerialNumber:       serialNumber,
		Subject:            pkix.Name{CommonName: DefaultCommonName},
		Issuer:             pkix.Name{CommonName: DefaultCommonName},
		SignatureAlgorithm: x509.PureEd25519,
		NotBefore:          time.Date(1975, time.January, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:           time.Date(4096, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, createSubjectAltNameExtension())

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, publicKey, privateKey)
	if err != nil {
		return nil, nil
	}

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})
}

func createSubjectAltNameExtension() pkix.Extension {
	return pkix.Extension{
		Id:    asn1.ObjectIdentifier{2, 5, 29, 17},
		Value: []byte{48, 6, 135, 4, 0, 0, 0, 0},
	}
}
