package telemetry

import (
	"errors"
	"time"

	"github.com/gagliardetto/solana-go"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

type Config struct {
	// TWAMPReflector is the reflector for TWAMP probes.
	TWAMPReflector twamplight.Reflector

	// PeerDiscovery is the configured peer discovery implementation.
	PeerDiscovery PeerDiscovery

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
}

func (c *Config) Validate() error {
	if c.TWAMPReflector == nil {
		return errors.New("twamp reflector is required")
	}
	if c.PeerDiscovery == nil {
		return errors.New("peer discovery is required")
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
	return nil
}
