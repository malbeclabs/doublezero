package bgpstatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
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
)

// BGPStatusExecutor submits a SetUserBGPStatus instruction onchain.
type BGPStatusExecutor interface {
	SetUserBGPStatus(ctx context.Context, u serviceability.UserBGPStatusUpdate) (solana.Signature, error)
}

// ServiceabilityClient fetches the current program state from the ledger.
type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// NamespaceCollector collects BGP session state and local network interfaces
// from a single Linux VRF network namespace. It returns the set of remote IP
// strings with ESTABLISHED BGP sessions, the local interfaces, and any error.
// Implement with DefaultCollector for production; use a mock in tests.
type NamespaceCollector func(ctx context.Context, namespace string) (established map[string]struct{}, ifaces []netutil.Interface, err error)

// Config holds all parameters for the BGP status submitter.
type Config struct {
	Log                     *slog.Logger
	Executor                BGPStatusExecutor
	ServiceabilityClient    ServiceabilityClient
	Collector               NamespaceCollector
	LocalDevicePK           solana.PublicKey
	BGPNamespace            string
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
	if c.BGPNamespace == "" {
		return errors.New("bgp namespace is required")
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
}

// --- Pure helpers (no Linux syscalls; fully testable on all platforms) ---

// buildEstablishedIPSet returns a set of remote IP strings for BGP sessions
// that are currently in the ESTABLISHED state.
func buildEstablishedIPSet(sockets []bgpSocket) map[string]struct{} {
	m := make(map[string]struct{}, len(sockets))
	for _, sock := range sockets {
		if sock.State == "ESTABLISHED" {
			m[sock.RemoteIP] = struct{}{}
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

// vrfNamespaces builds the list of Linux network namespaces to check for BGP
// sockets and tunnel interfaces. It derives additional namespaces from tenant
// VRF IDs by replacing the trailing numeric suffix of base (e.g. "ns-vrf1")
// with each tenant's VrfId. The base namespace is always included first.
func vrfNamespaces(base string, tenants []serviceability.Tenant) []string {
	prefix := strings.TrimRight(base, "0123456789")
	seen := map[string]struct{}{base: {}}
	nss := []string{base}
	for _, t := range tenants {
		if t.VrfId == 0 {
			continue
		}
		ns := prefix + strconv.FormatUint(uint64(t.VrfId), 10)
		if _, ok := seen[ns]; !ok {
			seen[ns] = struct{}{}
			nss = append(nss, ns)
		}
	}
	return nss
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
