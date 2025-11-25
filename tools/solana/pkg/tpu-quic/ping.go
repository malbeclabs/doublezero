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
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/quic-go/quic-go"
)

const (
	DefaultDuration = 10 * time.Second
	DefaultInterval = 3 * time.Second
	DefaultTimeout  = 3 * time.Second

	ALPNTPUProtocolID = "solana-tpu"
	DefaultCommonName = "Solana node"
)

var (
	DefaultSrc = net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}
)

type PingConfig struct {
	Context context.Context
	Logger  *slog.Logger

	// StatsChan that results will be streamed to as they are available.
	StatsChan chan *quic.ConnectionStats

	Quiet    bool          `json:"quiet"`
	Duration time.Duration `json:"duration"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
	Src      string        `json:"src"`
	Dst      string        `json:"dst"`
}

type PingResult struct {
	ConnectionStats []*quic.ConnectionStats `json:"connection_stats"`
	Success         bool                    `json:"success"`
	Error           error                   `json:"error"`
}

func (cfg *PingConfig) Validate() error {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if cfg.Logger == nil {
		var writer io.Writer
		if cfg.Quiet {
			writer = io.Discard
		} else {
			writer = os.Stdout
		}
		cfg.Logger = slog.New(slog.NewTextHandler(writer, nil))
	}
	if cfg.Duration == 0 {
		cfg.Duration = DefaultDuration
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Src == "" {
		cfg.Src = DefaultSrc.String()
	}
	if cfg.Dst == "" {
		return fmt.Errorf("dst address is required")
	}
	return nil
}

func Ping(cfg PingConfig) (*PingResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate run config: %w", err)
	}

	log := cfg.Logger
	ctx, cancel := context.WithTimeout(cfg.Context, cfg.Duration)
	defer cancel()

	// Resolve destination UDP address
	dstAddr, err := net.ResolveUDPAddr("udp", cfg.Dst)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote addr %s: %w", cfg.Dst, err)
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
	defer srcConn.Close()

	if !cfg.Quiet {
		log.Info("Connecting via QUIC", "src", srcConn.LocalAddr(), "dst", dstAddr)
	}

	quicConf := &quic.Config{
		MaxIdleTimeout:  cfg.Timeout,
		KeepAlivePeriod: cfg.Interval,
	}

	// Separate context for dial
	dialCtx, dialCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer dialCancel()

	// Create TLS config
	tlsConfig, err := createTlsConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config: %w", err)
	}

	// Dial QUIC session
	session, err := quic.Dial(dialCtx, srcConn, dstAddr, tlsConfig, quicConf)
	if err != nil {
		return &PingResult{
			Error: fmt.Errorf("failed to connect to %s from %s: %w", dstAddr, srcAddr, err),
		}, nil
	}
	defer func() {
		_ = session.CloseWithError(0, "client done")
	}()

	// Start stats ticker loop
	result := &PingResult{
		ConnectionStats: make([]*quic.ConnectionStats, 0, cfg.Duration/cfg.Interval),
		Success:         true,
		Error:           nil,
	}
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if !cfg.Quiet {
				log.Info("Completed QUIC ping session", "duration", cfg.Duration)
			}
			return result, nil
		case <-ticker.C:
			stats := session.ConnectionStats()
			result.ConnectionStats = append(result.ConnectionStats, &stats)
			if cfg.StatsChan != nil {
				select {
				case cfg.StatsChan <- &stats:
				default:
				}
			}
			if !cfg.Quiet {
				log.Info("QUIC stats",
					"rttMin", stats.MinRTT,
					"rttLatest", stats.LatestRTT,
					"rttSmoothed", stats.SmoothedRTT,
					"rttDev", stats.MeanDeviation,
					"sentBytes", stats.BytesSent,
					"sentPackets", stats.PacketsSent,
					"recvBytes", stats.BytesReceived,
					"recvPackets", stats.PacketsReceived,
					"lostBytes", stats.BytesLost,
					"lostPackets", stats.PacketsLost,
				)
			}
		}
	}
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
