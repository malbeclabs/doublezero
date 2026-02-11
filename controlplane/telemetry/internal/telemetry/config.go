package telemetry

import (
	"context"
	"errors"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

type Config struct {
	// TWAMPReflector is the reflector for TWAMP probes.
	TWAMPReflector twamplight.Reflector

	// PeerDiscovery is the configured peer discovery implementation.
	PeerDiscovery PeerDiscovery

	// GetCurrentEpochFunc is the function to get the current epoch.
	GetCurrentEpochFunc func(ctx context.Context) (uint64, error)

	// TelemetryProgramClient is the client to the telemetry program.
	TelemetryProgramClient TelemetryProgramClient

	// LocalDevicePK is the public key of the local device PDA onchain.
	LocalDevicePK solana.PublicKey

	// MetricsPublisherPK is the public key of the metrics publisher.
	MetricsPublisherPK solana.PublicKey

	// ProbeInterval is the interval at which to probe peers.
	ProbeInterval time.Duration

	// SubmissionInterval is the interval at which to submit samples.
	SubmissionInterval time.Duration

	// TWAMPSenderTimeout is the timeout for sending TWAMP probes.
	TWAMPSenderTimeout time.Duration

	// NowFunc is the function to get the current time.
	NowFunc func() time.Time

	// SenderTTL is the time to live for a sender instance until it's recreated.
	SenderTTL time.Duration

	// SubmitterMaxConcurrency is the maximum number of concurrent submissions.
	SubmitterMaxConcurrency int

	// InitialChildProbes is the startup probe list from CLI; runtime updates happen via channel.
	InitialChildGeoProbes []geoprobe.ProbeAddress

	// ServiceabilityProgramClient is the client to the serviceability program (for fetching Device/Location).
	ServiceabilityProgramClient *serviceability.Client

	// RPCClient is the Solana RPC client (for fetching slot).
	RPCClient *solanarpc.Client

	// Keypair is the metrics publisher keypair (for signing offsets).
	Keypair solana.PrivateKey
}

func (c *Config) Validate() error {
	if c.TWAMPReflector == nil {
		return errors.New("twamp reflector is required")
	}
	if c.PeerDiscovery == nil {
		return errors.New("peer discovery is required")
	}
	if c.GetCurrentEpochFunc == nil {
		return errors.New("get current epoch is required")
	}
	if c.LocalDevicePK.IsZero() {
		return errors.New("local device pubkey is required")
	}
	if c.ProbeInterval <= 0 {
		return errors.New("probe interval must be greater than 0")
	}
	if c.SubmissionInterval <= 0 {
		return errors.New("submission interval must be greater than 0")
	}
	if c.TWAMPSenderTimeout <= 0 {
		return errors.New("twamp sender timeout must be greater than 0")
	}
	if c.TelemetryProgramClient == nil {
		return errors.New("telemetry program client is required")
	}
	if c.NowFunc == nil {
		c.NowFunc = func() time.Time {
			return time.Now().UTC()
		}
	}
	if c.SenderTTL <= 0 {
		return errors.New("sender ttl must be greater than 0")
	}
	if c.SubmitterMaxConcurrency <= 0 {
		return errors.New("submitter max concurrency must be greater than 0")
	}

	if len(c.InitialChildGeoProbes) > 0 {
		if c.ServiceabilityProgramClient == nil {
			return errors.New("serviceability client is required when geoprobe is enabled")
		}
		if c.RPCClient == nil {
			return errors.New("rpc client is required when geoprobe is enabled")
		}
		if c.Keypair == nil {
			return errors.New("keypair is required when geoprobe is enabled")
		}
	}

	return nil
}
