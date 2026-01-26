package sol

import (
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

// LeaderScheduleSchema defines the schema for leader schedule entries
type LeaderScheduleSchema struct{}

func (s *LeaderScheduleSchema) Name() string {
	return "solana_leader_schedule"
}

func (s *LeaderScheduleSchema) PrimaryKeyColumns() []string {
	return []string{"node_pubkey:VARCHAR"}
}

func (s *LeaderScheduleSchema) PayloadColumns() []string {
	return []string{
		"epoch:BIGINT",
		"slots:VARCHAR",
		"slot_count:BIGINT",
	}
}

func (s *LeaderScheduleSchema) ToRow(entry LeaderScheduleEntry, epoch uint64) []any {
	slotsStr := formatUint64Array(entry.Slots)
	return []any{
		entry.NodePubkey.String(),
		epoch,
		slotsStr,
		int64(len(entry.Slots)),
	}
}

func (s *LeaderScheduleSchema) GetPrimaryKey(entry LeaderScheduleEntry) string {
	return entry.NodePubkey.String()
}

// VoteAccountSchema defines the schema for vote accounts
type VoteAccountSchema struct{}

func (s *VoteAccountSchema) Name() string {
	return "solana_vote_accounts"
}

func (s *VoteAccountSchema) PrimaryKeyColumns() []string {
	return []string{"vote_pubkey:VARCHAR"}
}

func (s *VoteAccountSchema) PayloadColumns() []string {
	return []string{
		"epoch:BIGINT",
		"node_pubkey:VARCHAR",
		"activated_stake_lamports:BIGINT",
		"epoch_vote_account:VARCHAR",
		"commission_percentage:BIGINT",
	}
}

func (s *VoteAccountSchema) ToRow(account solanarpc.VoteAccountsResult, epoch uint64) []any {
	epochVoteAccountStr := "false"
	if account.EpochVoteAccount {
		epochVoteAccountStr = "true"
	}
	return []any{
		account.VotePubkey.String(),
		epoch,
		account.NodePubkey.String(),
		account.ActivatedStake,
		epochVoteAccountStr,
		account.Commission,
	}
}

func (s *VoteAccountSchema) GetPrimaryKey(account solanarpc.VoteAccountsResult) string {
	return account.VotePubkey.String()
}

// GossipNodeSchema defines the schema for gossip nodes
type GossipNodeSchema struct{}

func (s *GossipNodeSchema) Name() string {
	return "solana_gossip_nodes"
}

func (s *GossipNodeSchema) PrimaryKeyColumns() []string {
	return []string{"pubkey:VARCHAR"}
}

func (s *GossipNodeSchema) PayloadColumns() []string {
	return []string{
		"epoch:BIGINT",
		"gossip_ip:VARCHAR",
		"gossip_port:INTEGER",
		"tpuquic_ip:VARCHAR",
		"tpuquic_port:INTEGER",
		"version:VARCHAR",
	}
}

func (s *GossipNodeSchema) ToRow(node *solanarpc.GetClusterNodesResult, epoch uint64) []any {
	var gossipIP, tpuQUICIP string
	var gossipPort, tpuQUICPort int32
	if node.Gossip != nil {
		host, portStr, err := net.SplitHostPort(*node.Gossip)
		if err == nil {
			gossipIP = host
			gossipPortUint, err := strconv.ParseUint(portStr, 10, 16)
			if err == nil {
				gossipPort = int32(gossipPortUint)
			}
		}
	}
	if node.TPUQUIC != nil {
		host, portStr, err := net.SplitHostPort(*node.TPUQUIC)
		if err == nil {
			tpuQUICIP = host
			tpuQUICPortUint, err := strconv.ParseUint(portStr, 10, 16)
			if err == nil {
				tpuQUICPort = int32(tpuQUICPortUint)
			}
		}
	}
	var version string
	if node.Version != nil {
		version = *node.Version
	}
	return []any{
		node.Pubkey.String(),
		int64(epoch),
		gossipIP,
		gossipPort,
		tpuQUICIP,
		tpuQUICPort,
		version,
	}
}

func (s *GossipNodeSchema) GetPrimaryKey(node *solanarpc.GetClusterNodesResult) string {
	if node == nil {
		return ""
	}
	return node.Pubkey.String()
}

// VoteAccountActivitySchema defines the schema for vote account activity fact table
type VoteAccountActivitySchema struct{}

func (s *VoteAccountActivitySchema) Name() string {
	return "solana_vote_account_activity"
}

func (s *VoteAccountActivitySchema) UniqueKeyColumns() []string {
	return []string{"event_ts", "vote_account_pubkey"}
}

func (s *VoteAccountActivitySchema) Columns() []string {
	return []string{
		"ingested_at:TIMESTAMP",
		"vote_account_pubkey:VARCHAR",
		"node_identity_pubkey:VARCHAR",
		"epoch:INTEGER",
		"root_slot:BIGINT",
		"last_vote_slot:BIGINT",
		"cluster_slot:BIGINT",
		"is_delinquent:BOOLEAN",
		"epoch_credits_json:VARCHAR",
		"credits_epoch:INTEGER",
		"credits_epoch_credits:BIGINT",
		"credits_delta:BIGINT",
		"activated_stake_lamports:BIGINT",
		"activated_stake_sol:DOUBLE",
		"commission:INTEGER",
		"collector_run_id:VARCHAR",
	}
}

func (s *VoteAccountActivitySchema) TimeColumn() string {
	return "event_ts"
}

func (s *VoteAccountActivitySchema) PartitionByTime() bool {
	return true
}

func (s *VoteAccountActivitySchema) Grain() string {
	return "1 minute"
}

func (s *VoteAccountActivitySchema) DedupMode() dataset.DedupMode {
	return dataset.DedupReplacing
}

func (s *VoteAccountActivitySchema) DedupVersionColumn() string {
	return "ingested_at"
}

func (s *VoteAccountActivitySchema) ToRow(entry VoteAccountActivityEntry, ingestedAt time.Time) []any {
	// Order matches FactTableConfigVoteAccountActivity columns
	// Handle nullable types by converting pointers to values or nil
	var creditsDelta any
	if entry.CreditsDelta != nil {
		creditsDelta = *entry.CreditsDelta
	}

	var activatedStakeLamports any
	if entry.ActivatedStakeLamports != nil {
		activatedStakeLamports = int64(*entry.ActivatedStakeLamports)
	}

	var activatedStakeSol any
	if entry.ActivatedStakeSol != nil {
		activatedStakeSol = *entry.ActivatedStakeSol
	}

	var commission any
	if entry.Commission != nil {
		commission = int32(*entry.Commission)
	}

	return []any{
		entry.Time.UTC(),          // event_ts
		ingestedAt,                // ingested_at
		entry.VoteAccountPubkey,   // vote_account_pubkey
		entry.NodeIdentityPubkey,  // node_identity_pubkey
		entry.Epoch,               // epoch
		entry.RootSlot,            // root_slot
		entry.LastVoteSlot,        // last_vote_slot
		entry.ClusterSlot,         // cluster_slot
		entry.IsDelinquent,        // is_delinquent
		entry.EpochCreditsJSON,    // epoch_credits_json
		entry.CreditsEpoch,        // credits_epoch
		entry.CreditsEpochCredits, // credits_epoch_credits
		creditsDelta,              // credits_delta (nullable)
		activatedStakeLamports,    // activated_stake_lamports (nullable)
		activatedStakeSol,         // activated_stake_sol (nullable)
		commission,                // commission (nullable)
		entry.CollectorRunID,      // collector_run_id
	}
}

type BlockProductionSchema struct{}

func (s *BlockProductionSchema) Name() string {
	return "solana_block_production"
}

func (s *BlockProductionSchema) UniqueKeyColumns() []string {
	return []string{"epoch", "leader_identity_pubkey"}
}

func (s *BlockProductionSchema) Columns() []string {
	return []string{
		"epoch:INTEGER",
		"ingested_at:TIMESTAMP",
		"leader_identity_pubkey:VARCHAR",
		"leader_slots_assigned_cum:BIGINT",
		"blocks_produced_cum:BIGINT",
	}
}

func (s *BlockProductionSchema) TimeColumn() string {
	return "event_ts"
}

func (s *BlockProductionSchema) PartitionByTime() bool {
	return true
}

func (s *BlockProductionSchema) Grain() string {
	return "1 minute"
}

func (s *BlockProductionSchema) DedupMode() dataset.DedupMode {
	return dataset.DedupReplacing
}

func (s *BlockProductionSchema) DedupVersionColumn() string {
	return "ingested_at"
}

func (s *BlockProductionSchema) ToRow(entry BlockProductionEntry, ingestedAt time.Time) []any {
	return []any{
		entry.Epoch,
		entry.Time.UTC(),
		ingestedAt,
		entry.LeaderIdentityPubkey,
		entry.LeaderSlotsAssignedCum,
		entry.BlocksProducedCum,
	}
}

var (
	leaderScheduleSchema      = &LeaderScheduleSchema{}
	voteAccountSchema         = &VoteAccountSchema{}
	gossipNodeSchema          = &GossipNodeSchema{}
	voteAccountActivitySchema = &VoteAccountActivitySchema{}
	blockProductionSchema     = &BlockProductionSchema{}
)

func NewBlockProductionDataset(log *slog.Logger) (*dataset.FactDataset, error) {
	return dataset.NewFactDataset(log, blockProductionSchema)
}

func NewVoteAccountActivityDataset(log *slog.Logger) (*dataset.FactDataset, error) {
	return dataset.NewFactDataset(log, voteAccountActivitySchema)
}

func NewLeaderScheduleDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, leaderScheduleSchema)
}

func NewVoteAccountDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, voteAccountSchema)
}

func NewGossipNodeDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, gossipNodeSchema)
}

// formatUint64Array formats a uint64 array as a JSON-like string
func formatUint64Array(arr []uint64) string {
	if len(arr) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, v := range arr {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.FormatUint(v, 10))
	}
	b.WriteString("]")
	return b.String()
}
