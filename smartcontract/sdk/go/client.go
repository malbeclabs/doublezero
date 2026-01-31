package dzsdk

import (
	"errors"
	"log/slog"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/sdk/revdist/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type Client struct {
	Serviceability      *serviceability.Client
	Telemetry           *telemetry.Client
	RevenueDistribution *revdist.Client
}

type Config struct {
	Endpoint                     string
	Signer                       *solana.PrivateKey
	ServiceabilityProgramID      solana.PublicKey
	TelemetryProgramID           solana.PublicKey
	RevenueDistributionProgramID solana.PublicKey
}

type Option func(*Config)

func New(log *slog.Logger, endpoint string, opts ...Option) (*Client, error) {
	cfg := &Config{
		Endpoint: endpoint,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.Endpoint == "" {
		return nil, errors.New("endpoint is required")
	}

	if cfg.ServiceabilityProgramID.IsZero() {
		cfg.ServiceabilityProgramID = solana.MustPublicKeyFromBase58(config.TestnetServiceabilityProgramID)
	}

	if cfg.TelemetryProgramID.IsZero() {
		cfg.TelemetryProgramID = solana.MustPublicKeyFromBase58(config.TestnetTelemetryProgramID)
	}

	rpcClient := solanarpc.New(cfg.Endpoint)

	c := &Client{
		Serviceability: serviceability.New(rpcClient, cfg.ServiceabilityProgramID),
		Telemetry:      telemetry.New(log, rpcClient, cfg.Signer, cfg.TelemetryProgramID),
	}

	if !cfg.RevenueDistributionProgramID.IsZero() {
		c.RevenueDistribution = revdist.New(rpcClient, cfg.RevenueDistributionProgramID)
	}

	return c, nil
}

// Configure the serviceability program ID.
func WithServiceabilityProgramID(programID string) Option {
	return func(c *Config) {
		c.ServiceabilityProgramID = solana.MustPublicKeyFromBase58(programID)
	}
}

// Configures the client with a private key for signing transactions.
func WithSigner(signer *solana.PrivateKey) Option {
	return func(c *Config) {
		c.Signer = signer
	}
}

// Configure the telemetry program ID.
func WithTelemetryProgramID(programID string) Option {
	return func(c *Config) {
		c.TelemetryProgramID = solana.MustPublicKeyFromBase58(programID)
	}
}

// Configure the revenue distribution program ID.
func WithRevenueDistributionProgramID(programID string) Option {
	return func(c *Config) {
		c.RevenueDistributionProgramID = solana.MustPublicKeyFromBase58(programID)
	}
}
