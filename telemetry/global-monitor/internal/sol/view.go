package sol

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type SolanaRPC interface {
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

type GossipNode struct {
	Pubkey      solana.PublicKey
	GossipIP    net.IP
	GossipPort  uint16
	TPUQUICIP   net.IP
	TPUQUICPort uint16
}

func (n *GossipNode) TPUQUICAddr() (string, bool) {
	if n.TPUQUICIP == nil || n.TPUQUICPort == 0 {
		return "", false
	}
	addr := net.JoinHostPort(n.TPUQUICIP.String(), strconv.Itoa(int(n.TPUQUICPort)))
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
	GeoIP       *geoip.Record
}

type SolanaView struct {
	log *slog.Logger
	rpc SolanaRPC

	geoIP geoip.Resolver
}

func NewSolanaView(
	log *slog.Logger,
	rpc SolanaRPC,
	geoIP geoip.Resolver,
) (*SolanaView, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if rpc == nil {
		return nil, fmt.Errorf("rpc is nil")
	}
	if geoIP == nil {
		return nil, fmt.Errorf("geoIP resolver is nil")
	}
	return &SolanaView{
		log:   log,
		rpc:   rpc,
		geoIP: geoIP,
	}, nil
}

func (v *SolanaView) GetValidatorsByNodePubkey(ctx context.Context) (map[solana.PublicKey]*Validator, error) {
	_, vals, err := v.GetGossipNodesAndValidatorsByNodePubkey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gossip nodes and validators: %w", err)
	}
	return vals, nil
}

func (v *SolanaView) GetGossipNodesAndValidatorsByNodePubkey(ctx context.Context) (map[solana.PublicKey]*GossipNode, map[solana.PublicKey]*Validator, error) {
	v.log.Debug("solana: retrieving gossip nodes and validators")

	leaderSchedule, err := v.rpc.GetLeaderSchedule(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get leader schedule: %w", err)
	}
	var totalLeaderSlots int
	for _, slots := range leaderSchedule {
		totalLeaderSlots += len(slots)
	}

	nodesFromRPC, err := v.rpc.GetClusterNodes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}
	nodesFromRPCByPubkey := make(map[solana.PublicKey]*solanarpc.GetClusterNodesResult, len(nodesFromRPC))
	nodes := make(map[solana.PublicKey]*GossipNode)
	for _, nodeFromRPC := range nodesFromRPC {
		node := v.gossipNodeFromRPC(nodeFromRPC)
		nodes[nodeFromRPC.Pubkey] = &node
		nodesFromRPCByPubkey[nodeFromRPC.Pubkey] = nodeFromRPC
	}

	voteAccountsRes, err := v.rpc.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get vote accounts: %w", err)
	}
	if voteAccountsRes == nil {
		return nil, nil, fmt.Errorf("vote accounts response is nil")
	}

	vals := make(map[solana.PublicKey]*Validator)

	for _, voteAccountFromRPC := range voteAccountsRes.Current {
		nodePK := voteAccountFromRPC.NodePubkey
		node, ok := nodes[nodePK]
		if !ok {
			v.log.Debug("solana: node not found for vote account", "pubkey", nodePK)
			continue
		}

		slots := leaderSchedule[nodePK]
		var leaderRatio float64
		if totalLeaderSlots > 0 {
			leaderRatio = float64(len(slots)) / float64(totalLeaderSlots)
		}

		voteAccount := VoteAccount{
			VotePubkey:     voteAccountFromRPC.VotePubkey,
			NodePubkey:     voteAccountFromRPC.NodePubkey,
			ActivatedStake: voteAccountFromRPC.ActivatedStake,
		}

		vals[node.Pubkey] = &Validator{
			Node:        *node,
			VoteAccount: voteAccount,
			LeaderRatio: leaderRatio,
			GeoIP:       v.geoIP.Resolve(node.GossipIP),
		}
	}

	return nodes, vals, nil
}

func (v *SolanaView) gossipNodeFromRPC(res *solanarpc.GetClusterNodesResult) GossipNode {
	var tpuquicIP net.IP
	var tpuquicPort uint16
	if res.TPUQUIC != nil {
		host, portStr, err := net.SplitHostPort(*res.TPUQUIC)
		if err == nil {
			parsedIP := net.ParseIP(host)
			if parsedIP != nil && parsedIP.To4() != nil {
				tpuquicIP = parsedIP.To4()
			}
			portUint, err := strconv.ParseUint(portStr, 10, 16)
			if err == nil {
				tpuquicPort = uint16(portUint)
			}
		}
	}

	var gossipIP net.IP
	var gossipPort uint16
	if res.Gossip != nil {
		host, portStr, err := net.SplitHostPort(*res.Gossip)
		if err == nil {
			parsedIP := net.ParseIP(host)
			if parsedIP != nil && parsedIP.To4() != nil {
				gossipIP = parsedIP.To4()
			}
			portUint, err := strconv.ParseUint(portStr, 10, 16)
			if err == nil {
				gossipPort = uint16(portUint)
			}
		}
	}

	return GossipNode{
		Pubkey:      res.Pubkey,
		GossipIP:    gossipIP,
		GossipPort:  gossipPort,
		TPUQUICIP:   tpuquicIP,
		TPUQUICPort: tpuquicPort,
	}
}
