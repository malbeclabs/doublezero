package bgpstatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	taskChannelCapacity    = 256
	defaultInterval        = 60 * time.Second
	defaultRefreshInterval = 6 * time.Hour
	submitMaxRetries       = 3
	submitBaseBackoff      = 100 * time.Millisecond
)

// BGPStatusExecutor submits a SetUserBGPStatus instruction onchain.
type BGPStatusExecutor interface {
	SetUserBGPStatus(ctx context.Context, u serviceability.UserBGPStatusUpdate) (solana.Signature, error)
}

// ServiceabilityClient fetches the current program state from the ledger.
type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// BGPCollector returns the set of remote IP strings with ESTABLISHED BGP
// sessions, across all network instances visible to the device. It is called
// once per tick. Implement with GNMICollector for production; use a mock in tests.
type BGPCollector func(ctx context.Context) (established map[string]struct{}, err error)

// Config holds all parameters for the BGP status submitter.
type Config struct {
	Log                     *slog.Logger
	Executor                BGPStatusExecutor
	ServiceabilityClient    ServiceabilityClient
	Collector               BGPCollector
	LocalDevicePK           solana.PublicKey
	Interval                time.Duration // default: 60s
	PeriodicRefreshInterval time.Duration // default: 6h
	DownGracePeriod         time.Duration // default: 0
	Clock                   clockwork.Clock
}

func (c *Config) validate() error {
	if c.Log == nil {
		return errors.New("log is required")
	}
	if c.Executor == nil {
		return errors.New("executor is required")
	}
	if c.ServiceabilityClient == nil {
		return errors.New("serviceability client is required")
	}
	if c.Collector == nil {
		return errors.New("collector is required")
	}
	if c.LocalDevicePK.IsZero() {
		return errors.New("local device pubkey is required")
	}
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if c.PeriodicRefreshInterval <= 0 {
		c.PeriodicRefreshInterval = defaultRefreshInterval
	}
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	return nil
}

// userState tracks submission state for a single user.
type userState struct {
	lastOnchainStatus serviceability.BGPStatus
	lastWriteTime     time.Time
	lastUpObservedAt  time.Time
}

// submitTask is queued to the background worker for onchain submission.
type submitTask struct {
	user   serviceability.User
	status serviceability.BGPStatus
}

// Submitter collects BGP session state on each tick, determines per-user BGP
// status, and submits SetUserBGPStatus onchain via a non-blocking worker.
type Submitter struct {
	cfg       Config
	log       *slog.Logger
	userState map[string]*userState // keyed by user PubKey base58
	pending   map[string]bool       // users currently in-flight in the worker
	mu        sync.Mutex
	taskCh    chan submitTask
}

// NewSubmitter creates a Submitter after validating the config.
func NewSubmitter(cfg Config) (*Submitter, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid bgpstatus config: %w", err)
	}
	return &Submitter{
		cfg:       cfg,
		log:       cfg.Log,
		userState: make(map[string]*userState),
		pending:   make(map[string]bool),
		taskCh:    make(chan submitTask, taskChannelCapacity),
	}, nil
}

// Start launches the submitter in the background and returns a channel that
// receives a fatal error (or is closed on clean shutdown).
func (s *Submitter) Start(ctx context.Context, cancel context.CancelFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer cancel()
		if err := s.run(ctx); err != nil {
			s.log.Error("bgpstatus: submitter failed", "error", err)
			errCh <- err
		}
	}()
	return errCh
}

// userStateFor returns or creates the per-user tracking entry (caller must hold s.mu).
// initialStatus seeds lastOnchainStatus so a restarted submitter correctly handles
// users whose onchain state is already Up.
func (s *Submitter) userStateFor(key string, initialStatus serviceability.BGPStatus) *userState {
	us, ok := s.userState[key]
	if !ok {
		us = &userState{lastOnchainStatus: initialStatus}
		s.userState[key] = us
	}
	return us
}

// --- Pure helpers (no platform-specific code; fully testable on all platforms) ---

// tunnelNetToIPNet parses the onchain [5]byte tunnel-net encoding into a
// *net.IPNet. The format is [4 bytes IPv4 prefix | 1 byte CIDR length].
func tunnelNetToIPNet(b [5]byte) *net.IPNet {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return &net.IPNet{IP: ip.To4(), Mask: mask}
}

// peerIPsFor31 returns both host IPs in a /31 network. Since tunnel IPs are
// globally unique (onchain-allocated), exactly one of the two will be the
// BGP neighbor address for a given user on this device.
func peerIPsFor31(tunnelNet *net.IPNet) (net.IP, net.IP) {
	ip0 := tunnelNet.IP.To4()
	ip1 := make(net.IP, 4)
	copy(ip1, ip0)
	ip1[3] ^= 1
	return ip0, ip1
}

// computeEffectiveStatus derives the BGP status to report, applying the down
// grace period: if observedUp is false but the user was last seen Up within
// gracePeriod, we still report Up to avoid transient flaps.
func computeEffectiveStatus(
	observedUp bool,
	us *userState,
	now time.Time,
	gracePeriod time.Duration,
) serviceability.BGPStatus {
	if observedUp {
		return serviceability.BGPStatusUp
	}
	if us.lastUpObservedAt.IsZero() {
		return serviceability.BGPStatusDown
	}
	if gracePeriod > 0 && now.Sub(us.lastUpObservedAt) < gracePeriod {
		return serviceability.BGPStatusUp
	}
	return serviceability.BGPStatusDown
}

// shouldSubmit returns true when a submission is warranted: either the status
// has changed from what was last confirmed onchain, or it is time for a
// periodic keepalive write.
func shouldSubmit(
	us *userState,
	newStatus serviceability.BGPStatus,
	now time.Time,
	refreshInterval time.Duration,
) bool {
	if us.lastWriteTime.IsZero() {
		return true
	}
	if us.lastOnchainStatus != newStatus {
		return true
	}
	return now.Sub(us.lastWriteTime) >= refreshInterval
}
