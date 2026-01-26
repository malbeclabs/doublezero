package sol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

type StoreConfig struct {
	Logger     *slog.Logger
	ClickHouse clickhouse.Client
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	return nil
}

type Store struct {
	log *slog.Logger
	cfg StoreConfig
}

func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

type LeaderScheduleEntry struct {
	NodePubkey solana.PublicKey
	Slots      []uint64
}

func (s *Store) ReplaceLeaderSchedule(ctx context.Context, entries []LeaderScheduleEntry, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing leader schedule", "count", len(entries))

	d, err := NewLeaderScheduleDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	// Write to ClickHouse
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse using new dataset API
	err = d.WriteBatch(ctx, conn, len(entries), func(i int) ([]any, error) {
		return leaderScheduleSchema.ToRow(entries[i], currentEpoch), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	})
	if err != nil {
		return fmt.Errorf("failed to write leader schedule to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceVoteAccounts(ctx context.Context, accounts []solanarpc.VoteAccountsResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing vote accounts", "count", len(accounts))

	d, err := NewVoteAccountDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	// Write to ClickHouse
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse using new dataset API
	err = d.WriteBatch(ctx, conn, len(accounts), func(i int) ([]any, error) {
		return voteAccountSchema.ToRow(accounts[i], currentEpoch), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	})
	if err != nil {
		return fmt.Errorf("failed to write vote accounts to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceGossipNodes(ctx context.Context, nodes []*solanarpc.GetClusterNodesResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing gossip nodes", "count", len(nodes))

	// Filter out nodes with invalid/zero pubkeys to prevent NULL/empty primary keys
	validNodes := make([]*solanarpc.GetClusterNodesResult, 0, len(nodes))
	for i, node := range nodes {
		if node.Pubkey.IsZero() {
			s.log.Warn("solana/store: skipping node with zero pubkey", "index", i)
			continue
		}
		pubkeyStr := node.Pubkey.String()
		if pubkeyStr == "" {
			s.log.Warn("solana/store: skipping node with empty pubkey string", "index", i)
			continue
		}
		validNodes = append(validNodes, node)
	}

	if len(validNodes) != len(nodes) {
		s.log.Warn("solana/store: filtered out invalid nodes",
			"original_count", len(nodes),
			"valid_count", len(validNodes),
			"filtered_count", len(nodes)-len(validNodes))
	}

	d, err := NewGossipNodeDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	// Write to ClickHouse
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse using new dataset API
	err = d.WriteBatch(ctx, conn, len(validNodes), func(i int) ([]any, error) {
		return gossipNodeSchema.ToRow(validNodes[i], currentEpoch), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	})
	if err != nil {
		return fmt.Errorf("failed to write gossip nodes to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) GetGossipIPs(ctx context.Context) ([]net.IP, error) {
	// Query ClickHouse history table to get current gossip_ip values
	// Uses deterministic "latest row per entity" definition
	query := `
		WITH ranked AS (
			SELECT
				*,
				ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
			FROM dim_solana_gossip_nodes_history
		)
		SELECT DISTINCT gossip_ip
		FROM ranked
		WHERE rn = 1 AND is_deleted = 0 AND gossip_ip != '' AND gossip_ip IS NOT NULL
	`
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query gossip IPs: %w", err)
	}
	defer rows.Close()

	var ips []net.IP
	for rows.Next() {
		var ipStr string
		if err := rows.Scan(&ipStr); err != nil {
			return nil, fmt.Errorf("failed to scan gossip IP: %w", err)
		}
		ip := net.ParseIP(ipStr)
		if ip != nil {
			ips = append(ips, ip)
		}
	}

	return ips, nil
}

type VoteAccountActivityEntry struct {
	Time                   time.Time
	VoteAccountPubkey      string
	NodeIdentityPubkey     string
	Epoch                  uint32
	RootSlot               uint64
	LastVoteSlot           uint64
	ClusterSlot            uint64
	IsDelinquent           bool
	EpochCreditsJSON       string
	CreditsEpoch           int
	CreditsEpochCredits    uint64
	CreditsDelta           *int64
	ActivatedStakeLamports *uint64
	ActivatedStakeSol      *float64
	Commission             *uint8
	CollectorRunID         string
}

type previousCreditsState struct {
	Epoch        int
	EpochCredits uint64
}

// GetPreviousCreditsStatesBatch retrieves the previous credits state for multiple vote accounts in a single query
func (s *Store) GetPreviousCreditsStatesBatch(ctx context.Context, voteAccountPubkeys []string) (map[string]*previousCreditsState, error) {
	if len(voteAccountPubkeys) == 0 {
		return make(map[string]*previousCreditsState), nil
	}

	// Build query with IN clause for all vote account pubkeys
	placeholders := make([]string, len(voteAccountPubkeys))
	args := make([]any, len(voteAccountPubkeys))
	for i, pubkey := range voteAccountPubkeys {
		placeholders[i] = "?"
		args[i] = pubkey
	}

	// Use ROW_NUMBER to get the latest row per vote_account_pubkey
	query := fmt.Sprintf(`SELECT vote_account_pubkey, credits_epoch, credits_epoch_credits
		FROM (
			SELECT
				vote_account_pubkey,
				credits_epoch,
				credits_epoch_credits,
				ROW_NUMBER() OVER (PARTITION BY vote_account_pubkey ORDER BY event_ts DESC) AS rn
			FROM fact_solana_vote_account_activity
			WHERE vote_account_pubkey IN (%s)
		) ranked
		WHERE rn = 1`,
		strings.Join(placeholders, ","))

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous credits states: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*previousCreditsState)
	for rows.Next() {
		var pubkey string
		var epoch *int32        // credits_epoch is INTEGER (Int32), not Int64
		var epochCredits *int64 // credits_epoch_credits is BIGINT (Int64), scan as int64 then convert to uint64

		if err := rows.Scan(&pubkey, &epoch, &epochCredits); err != nil {
			return nil, fmt.Errorf("failed to scan previous credits state: %w", err)
		}

		if epoch != nil && epochCredits != nil {
			result[pubkey] = &previousCreditsState{
				Epoch:        int(*epoch),
				EpochCredits: uint64(*epochCredits),
			}
		}
	}

	return result, nil
}

func calculateCreditsDelta(currentEpoch int, currentCredits uint64, prev *previousCreditsState) *int64 {
	if prev == nil {
		return nil // First observation
	}

	if currentEpoch == prev.Epoch {
		// Same epoch: max(C - C_prev, 0)
		if currentCredits >= prev.EpochCredits {
			delta := int64(currentCredits - prev.EpochCredits)
			return &delta
		}
		delta := int64(0)
		return &delta
	}

	if currentEpoch == prev.Epoch+1 {
		// Epoch rollover: cannot calculate meaningful delta across epochs
		return nil
	}

	// Any other jump/gap: NULL
	return nil
}

func (s *Store) InsertVoteAccountActivity(ctx context.Context, entries []VoteAccountActivityEntry) error {
	if len(entries) == 0 {
		return nil
	}

	s.log.Debug("solana/store: inserting vote account activity", "count", len(entries))

	// Get previous state for all vote accounts in a single batch query
	voteAccountPubkeys := make([]string, 0, len(entries))
	pubkeySet := make(map[string]bool)
	for _, entry := range entries {
		if !pubkeySet[entry.VoteAccountPubkey] {
			voteAccountPubkeys = append(voteAccountPubkeys, entry.VoteAccountPubkey)
			pubkeySet[entry.VoteAccountPubkey] = true
		}
	}

	prevStateMap, err := s.GetPreviousCreditsStatesBatch(ctx, voteAccountPubkeys)
	if err != nil {
		return fmt.Errorf("failed to get previous credits states: %w", err)
	}

	// Calculate credits_delta for each entry
	for i := range entries {
		entry := &entries[i]
		prev := prevStateMap[entry.VoteAccountPubkey]
		entry.CreditsDelta = calculateCreditsDelta(entry.CreditsEpoch, entry.CreditsEpochCredits, prev)
	}

	// Write to ClickHouse
	ingestedAt := time.Now().UTC()
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	ds, err := NewVoteAccountActivityDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create fact dataset: %w", err)
	}
	if err := ds.WriteBatch(ctx, conn, len(entries), func(i int) ([]any, error) {
		return voteAccountActivitySchema.ToRow(entries[i], ingestedAt), nil
	}); err != nil {
		return fmt.Errorf("failed to write vote account activity to ClickHouse: %w", err)
	}

	return nil
}

type BlockProductionEntry struct {
	Epoch                  int
	Time                   time.Time
	LeaderIdentityPubkey   string
	LeaderSlotsAssignedCum uint64
	BlocksProducedCum      uint64
}

func (s *Store) InsertBlockProduction(ctx context.Context, entries []BlockProductionEntry) error {
	if len(entries) == 0 {
		return nil
	}

	s.log.Debug("solana/store: inserting block production", "count", len(entries))

	// Write to ClickHouse
	ingestedAt := time.Now().UTC()
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	ds, err := NewBlockProductionDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create fact dataset: %w", err)
	}
	if err := ds.WriteBatch(ctx, conn, len(entries), func(i int) ([]any, error) {
		entry := entries[i]
		return []any{
			int32(entry.Epoch), // epoch (Int32 in migration)
			entry.Time.UTC(),   // event_ts
			ingestedAt,         // ingested_at
			entry.LeaderIdentityPubkey,
			int64(entry.LeaderSlotsAssignedCum), // Int64 in migration
			int64(entry.BlocksProducedCum),      // Int64 in migration
		}, nil
	}); err != nil {
		return fmt.Errorf("failed to write block production to ClickHouse: %w", err)
	}

	return nil
}
