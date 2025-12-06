package sol

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/geoip"
)

type SolanaRPC interface {
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

type GossipNode struct {
	Pubkey      solana.PublicKey
	IP          net.IP
	TPUQUICPort uint16
}

func (n *GossipNode) TPUQUICAddr() (string, bool) {
	if n.IP == nil || n.TPUQUICPort == 0 {
		return "", false
	}
	addr := net.JoinHostPort(n.IP.String(), strconv.Itoa(int(n.TPUQUICPort)))
	return addr, true
}

type VoteAccount struct {
	VotePubkey     solana.PublicKey
	NodePubkey     solana.PublicKey
	ActivatedStake uint64
}

type Validator struct {
	Node        GossipNode
	VoteAccount VoteAccount
	LeaderRatio float64 // ratio of slots the validator is leader for
	GeoIP       *geoip.GeoIP
}

type ValidatorsView struct {
	log *slog.Logger
	rpc SolanaRPC

	geoIP geoip.Resolver
}

func NewValidatorsView(
	log *slog.Logger,
	rpc SolanaRPC,
	geoIP geoip.Resolver,
) (*ValidatorsView, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if rpc == nil {
		return nil, fmt.Errorf("rpc is nil")
	}
	if geoIP == nil {
		return nil, fmt.Errorf("geoIP resolver is nil")
	}
	return &ValidatorsView{
		log:   log,
		rpc:   rpc,
		geoIP: geoIP,
	}, nil
}

func (v *ValidatorsView) GetValidatorsByNodePubkey(ctx context.Context) (map[solana.PublicKey]*Validator, error) {
	v.log.Debug("solana: retrieving validators")

	leaderSchedule, err := v.rpc.GetLeaderSchedule(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get leader schedule: %w", err)
	}
	var totalLeaderSlots int
	for _, slots := range leaderSchedule {
		totalLeaderSlots += len(slots)
	}

	nodesFromRPC, err := v.rpc.GetClusterNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}
	nodesFromRPCByPubkey := make(map[solana.PublicKey]*solanarpc.GetClusterNodesResult, len(nodesFromRPC))
	for _, node := range nodesFromRPC {
		nodesFromRPCByPubkey[node.Pubkey] = node
	}

	voteAccountsRes, err := v.rpc.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get vote accounts: %w", err)
	}
	if voteAccountsRes == nil {
		return nil, fmt.Errorf("vote accounts response is nil")
	}

	vals := make(map[solana.PublicKey]*Validator)

	for _, voteAccountFromRPC := range voteAccountsRes.Current {
		nodePK := voteAccountFromRPC.NodePubkey
		nodeFromRPC, ok := nodesFromRPCByPubkey[nodePK]
		if !ok {
			v.log.Debug("solana: node not found for vote account", "pubkey", nodePK)
			continue
		}

		slots := leaderSchedule[nodePK]
		var leaderRatio float64
		if totalLeaderSlots > 0 {
			leaderRatio = float64(len(slots)) / float64(totalLeaderSlots)
		}

		node := v.gossipNodeFromRPC(nodeFromRPC)

		voteAccount := VoteAccount{
			VotePubkey:     voteAccountFromRPC.VotePubkey,
			NodePubkey:     voteAccountFromRPC.NodePubkey,
			ActivatedStake: voteAccountFromRPC.ActivatedStake,
		}

		vals[node.Pubkey] = &Validator{
			Node:        node,
			VoteAccount: voteAccount,
			LeaderRatio: leaderRatio,
			GeoIP:       v.geoIP.Resolve(node.IP),
		}
	}

	return vals, nil
}

func (v *ValidatorsView) gossipNodeFromRPC(res *solanarpc.GetClusterNodesResult) GossipNode {
	var ip net.IP
	var port uint16
	if res.TPUQUIC != nil {
		host, portStr, err := net.SplitHostPort(*res.TPUQUIC)
		if err == nil {
			parsedIP := net.ParseIP(host)
			if parsedIP != nil && parsedIP.To4() != nil {
				ip = parsedIP.To4()
			}
			portUint, err := strconv.ParseUint(portStr, 10, 16)
			if err == nil {
				port = uint16(portUint)
			}
		}
	}
	return GossipNode{
		Pubkey:      res.Pubkey,
		IP:          ip,
		TPUQUICPort: port,
	}
}
