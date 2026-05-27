// Package exec wires the serviceability SDK behind the sweep.Executor
// interface. The orchestrator binary uses it against a real RPC; tests in
// pkg/sweep use a fake to avoid the network.
package exec

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/sweep"
)

// Config bundles the inputs the live executor needs.
type Config struct {
	Client   *serviceability.Client
	Executor *serviceability.Executor
	// RPC is used to fetch individual User accounts post-create so the
	// orchestrator can record the assigned TunnelId in the runlog. In
	// production this is the same *solanarpc.Client the Client/Executor
	// were built from.
	RPC serviceability.RPCClient

	DevicePubkey solana.PublicKey
	TenantPubkey solana.PublicKey // zero pubkey = no tenant

	// ClientIPBase is the starting /16 block from which sequential per-user
	// IPs are drawn. For idx i, the assigned IP is ClientIPBase + i.
	ClientIPBase [4]byte
	// TunnelEndpoint is passed through to UserCreateArgs verbatim; pass
	// 0.0.0.0 to use the device's public IP.
	TunnelEndpoint [4]byte
	// UserType / CyoaType pin the user kind for the entire sweep.
	UserType serviceability.UserUserType
	CyoaType serviceability.CyoaType
	// DzPrefixCount must match the device's dz_prefixes length; 1 is the
	// stress-test default.
	DzPrefixCount uint8
}

// Live implements sweep.Executor against a real serviceability program.
type Live struct {
	cfg Config
}

// New returns a Live executor with the given configuration. Callers must
// supply a non-nil Client, Executor, and RPC.
func New(cfg Config) (*Live, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("exec.New: Client is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("exec.New: Executor is required")
	}
	if cfg.RPC == nil {
		return nil, fmt.Errorf("exec.New: RPC is required")
	}
	if cfg.DzPrefixCount == 0 {
		cfg.DzPrefixCount = 1
	}
	return &Live{cfg: cfg}, nil
}

// ListUsers returns the current set of User accounts in the program. The
// caller (sweep loop) filters by owner via PlanFor.
func (l *Live) ListUsers(ctx context.Context) ([]serviceability.User, error) {
	pd, err := l.cfg.Client.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return pd.Users, nil
}

// CreateUser issues a CreateUser instruction for the idx-th stress user and
// records timestamps the sweep loop turns into runlog rows.
func (l *Live) CreateUser(ctx context.Context, idx int) (sweep.CreateResult, error) {
	args := serviceability.UserCreateArgs{
		UserType:       l.cfg.UserType,
		CyoaType:       l.cfg.CyoaType,
		ClientIP:       ipForIndex(l.cfg.ClientIPBase, idx),
		TunnelEndpoint: l.cfg.TunnelEndpoint,
		DzPrefixCount:  l.cfg.DzPrefixCount,
		DevicePubkey:   l.cfg.DevicePubkey,
		TenantPubkey:   l.cfg.TenantPubkey,
	}
	_, userPDA, err := l.cfg.Executor.CreateUser(ctx, args)
	if err != nil {
		return sweep.CreateResult{}, err
	}
	now := time.Now()

	// The SDK's CreateUser blocks on signature finalization and post-confirm
	// account visibility; we don't get distinct stage timestamps today, so
	// confirm and activate both anchor at the post-call wallclock. A future
	// SDK refactor can split these.
	//
	// Read the assigned tunnel_id back from the on-chain User account so the
	// runlog and the agent-event consumer can key on it. A failure here is
	// non-fatal: the create itself succeeded (the User exists onchain), so we
	// record TunnelID 0 and continue.
	tunnelID, err := l.fetchTunnelID(ctx, userPDA)
	if err != nil {
		tunnelID = 0
	}
	return sweep.CreateResult{
		UserPDA:     userPDA,
		TunnelID:    tunnelID,
		ConfirmedAt: now,
		ActivatedAt: now,
	}, nil
}

// DeleteUser closes a user account by PDA.
func (l *Live) DeleteUser(ctx context.Context, userPDA solana.PublicKey) (sweep.DeleteResult, error) {
	if _, err := l.cfg.Executor.DeleteUser(ctx, userPDA); err != nil {
		return sweep.DeleteResult{}, err
	}
	now := time.Now()
	return sweep.DeleteResult{
		ConfirmedAt: now,
		ActivatedAt: now,
	}, nil
}

// fetchTunnelID reads the user account by PDA and returns the assigned
// TunnelId. The sweep loop logs this in the runlog so the agent-event
// consumer can attribute `+ interface Tunnel<ID>` log lines back to a user.
func (l *Live) fetchTunnelID(ctx context.Context, userPDA solana.PublicKey) (uint16, error) {
	info, err := l.cfg.RPC.GetAccountInfo(ctx, userPDA)
	if err != nil {
		return 0, fmt.Errorf("get user account info: %w", err)
	}
	if info == nil || info.Value == nil {
		return 0, fmt.Errorf("user account %s not found", userPDA)
	}
	data := info.Value.Data.GetBinary()
	if len(data) == 0 {
		return 0, fmt.Errorf("user account %s empty", userPDA)
	}
	var u serviceability.User
	serviceability.DeserializeUser(serviceability.NewByteReader(data), &u)
	return u.TunnelId, nil
}

// ipForIndex returns base shifted by idx, wrapping at the /16 boundary so the
// 0..65535 range is usable without overflow handling on the caller side.
func ipForIndex(base [4]byte, idx int) [4]byte {
	host := uint32(base[2])<<8 | uint32(base[3])
	host += uint32(uint16(idx))
	var out [4]byte
	out[0] = base[0]
	out[1] = base[1]
	binary.BigEndian.PutUint16(out[2:], uint16(host))
	return out
}
