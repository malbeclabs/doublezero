package bgpstatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	taskChannelCapacity    = 256
	defaultInterval        = 60 * time.Second
	defaultRefreshInterval = 6 * time.Hour
	submitMaxRetries       = 3
	submitBaseBackoff      = 100 * time.Millisecond
	defaultNetnsDir        = "/var/run/netns"
)

// BGPStatusExecutor submits a SetUserBGPStatus instruction onchain.
type BGPStatusExecutor interface {
	SetUserBGPStatus(ctx context.Context, u serviceability.UserBGPStatusUpdate) (solana.Signature, error)
}

// ServiceabilityClient fetches the current program state from the ledger.
type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// BGPPeerState captures the per-peer BGP session info we want to surface onchain
// for each user. Keyed by remote IP string in the maps returned by NamespaceCollector.
type BGPPeerState struct {
	// Established is true when the peer's BGP TCP session is in ESTABLISHED state.
	Established bool
	// RttNs is the smoothed BGP TCP RTT in nanoseconds, sourced from the kernel's
	// tcp_info struct via INET_DIAG. 0 means no sample yet.
	RttNs uint64
}

// NamespaceCollector collects BGP session state and local network interfaces
// from a single Linux VRF network namespace. The returned map is keyed by the
// peer remote IP string and carries both Established and RttNs per peer.
// Implement with DefaultCollector for production; use a mock in tests.
type NamespaceCollector func(ctx context.Context, namespace string) (peers map[string]BGPPeerState, ifaces []netutil.Interface, err error)

// Config holds all parameters for the BGP status submitter.
type Config struct {
	Log                     *slog.Logger
	Executor                BGPStatusExecutor
	ServiceabilityClient    ServiceabilityClient
	Collector               NamespaceCollector
	LocalDevicePK           solana.PublicKey
	NetnsDir                string        // default: /var/run/netns
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
	if c.NetnsDir == "" {
		c.NetnsDir = defaultNetnsDir
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
	// rttNs is the smoothed BGP TCP RTT in nanoseconds to write alongside status.
	// 0 when the session is Down or no sample is available.
	rttNs uint64
}

// Submitter collects BGP socket state on each tick, determines per-user BGP
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
// receives a fatal error (or is closed on clean shutdown).  It mirrors the
// state.Collector.Start pattern.
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
// initialStatus is used only when creating a new entry; it seeds lastOnchainStatus so
// that a restarted submitter correctly handles users whose onchain state is already Up.
func (s *Submitter) userStateFor(key string, initialStatus serviceability.BGPStatus) *userState {
	us, ok := s.userState[key]
	if !ok {
		us = &userState{lastOnchainStatus: initialStatus}
		s.userState[key] = us
	}
	return us
}

// bgpSocket is the minimal BGP socket representation used by the pure helpers.
// The Linux-specific submitter.go converts state.BGPSocketState to this type.
type bgpSocket struct {
	RemoteIP string
	State    string
	// RttNs is the smoothed BGP TCP RTT in nanoseconds (kernel tcp_info.rtt
	// is microseconds; convert at the boundary). 0 means no sample.
	RttNs uint64
}

// --- Pure helpers (no Linux syscalls; fully testable on all platforms) ---

// buildPeerStateMap returns a map from remote IP to BGPPeerState for the given
// socket slice. Only sockets with State == "ESTABLISHED" mark Established=true;
// every observed peer still gets its RTT recorded so we can submit on a tunnel
// going Up later.
func buildPeerStateMap(sockets []bgpSocket) map[string]BGPPeerState {
	m := make(map[string]BGPPeerState, len(sockets))
	for _, sock := range sockets {
		m[sock.RemoteIP] = BGPPeerState{
			Established: sock.State == "ESTABLISHED",
			RttNs:       sock.RttNs,
		}
	}
	return m
}

// tunnelNetToIPNet parses the onchain [5]byte tunnel-net encoding into a
// *net.IPNet.  The format is [4 bytes IPv4 prefix | 1 byte CIDR length].
func tunnelNetToIPNet(b [5]byte) *net.IPNet {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return &net.IPNet{IP: ip.To4(), Mask: mask}
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

// listNamespaces returns the names of every entry under dir (typically
// /var/run/netns/). Each entry corresponds to one Linux network namespace
// the kernel currently exposes. Subdirectories and broken symlinks are
// included by name and left for the collector to handle, so we never miss
// a VRF because of a filtering rule that has drifted from kernel behavior.
func listNamespaces(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read netns dir %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
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
