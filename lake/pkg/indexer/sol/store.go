package sol

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
)

type StoreConfig struct {
	Logger *slog.Logger
	DB     duck.DB
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.DB == nil {
		return errors.New("db is required")
	}
	return nil
}

type Store struct {
	log *slog.Logger
	cfg StoreConfig
	db  duck.DB
}

func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
		db:  cfg.DB,
	}, nil
}

type LeaderScheduleEntry struct {
	NodePubkey solana.PublicKey
	Slots      []uint64
}

// SCD2ConfigLeaderSchedule returns the base SCD2 config for leader schedule table
func SCD2ConfigLeaderSchedule() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "solana_leader_schedule",
		PrimaryKeyColumns:   []string{"node_pubkey:VARCHAR"},
		PayloadColumns:      []string{"epoch:BIGINT", "slots:VARCHAR", "slot_count:BIGINT"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceLeaderSchedule(ctx context.Context, entries []LeaderScheduleEntry, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing leader schedule", "count", len(entries))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := SCD2ConfigLeaderSchedule()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("leader_schedule_%d_%d", currentEpoch, fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(entries), func(w *csv.Writer, i int) error {
		entry := entries[i]
		slotsStr := formatUint64Array(entry.Slots)
		return w.Write([]string{
			entry.NodePubkey.String(),
			fmt.Sprintf("%d", currentEpoch),
			slotsStr,
			fmt.Sprintf("%d", len(entry.Slots)),
		})
	})
}

// SCD2ConfigVoteAccounts returns the base SCD2 config for vote accounts table
func SCD2ConfigVoteAccounts() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "solana_vote_accounts",
		PrimaryKeyColumns:   []string{"vote_pubkey:VARCHAR"},
		PayloadColumns:      []string{"epoch:BIGINT", "node_pubkey:VARCHAR", "activated_stake_lamports:BIGINT", "epoch_vote_account:VARCHAR", "commission_percentage:BIGINT"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceVoteAccounts(ctx context.Context, accounts []solanarpc.VoteAccountsResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing vote accounts", "count", len(accounts))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := SCD2ConfigVoteAccounts()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("vote_accounts_%d_%d", currentEpoch, fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(accounts), func(w *csv.Writer, i int) error {
		account := accounts[i]
		epochVoteAccountStr := "false"
		if account.EpochVoteAccount {
			epochVoteAccountStr = "true"
		}
		return w.Write([]string{
			account.VotePubkey.String(),
			fmt.Sprintf("%d", currentEpoch),
			account.NodePubkey.String(),
			fmt.Sprintf("%d", account.ActivatedStake),
			epochVoteAccountStr,
			fmt.Sprintf("%d", account.Commission),
		})
	})
}

// SCD2ConfigGossipNodes returns the base SCD2 config for gossip nodes table
func SCD2ConfigGossipNodes() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "solana_gossip_nodes",
		PrimaryKeyColumns:   []string{"pubkey:VARCHAR"},
		PayloadColumns:      []string{"epoch:BIGINT", "gossip_ip:VARCHAR", "gossip_port:INTEGER", "tpuquic_ip:VARCHAR", "tpuquic_port:INTEGER", "version:VARCHAR"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceGossipNodes(ctx context.Context, nodes []*solanarpc.GetClusterNodesResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing gossip nodes", "count", len(nodes))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := SCD2ConfigGossipNodes()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("gossip_nodes_%d_%d", currentEpoch, fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(nodes), func(w *csv.Writer, i int) error {
		node := nodes[i]
		var gossipIP, tpuQUICIP string
		var gossipPort, tpuQUICPort uint16
		if node.Gossip != nil {
			host, portStr, err := net.SplitHostPort(*node.Gossip)
			if err == nil {
				gossipIP = host
				gossipPortUint, err := strconv.ParseUint(portStr, 10, 16)
				if err == nil {
					gossipPort = uint16(gossipPortUint)
				}
			}
		}
		if node.TPUQUIC != nil {
			host, portStr, err := net.SplitHostPort(*node.TPUQUIC)
			if err == nil {
				tpuQUICIP = host
				tpuQUICPortUint, err := strconv.ParseUint(portStr, 10, 16)
				if err == nil {
					tpuQUICPort = uint16(tpuQUICPortUint)
				}
			}
		}
		var version string
		if node.Version != nil {
			version = *node.Version
		}
		return w.Write([]string{
			node.Pubkey.String(),
			fmt.Sprintf("%d", currentEpoch),
			gossipIP,
			fmt.Sprintf("%d", gossipPort),
			tpuQUICIP,
			fmt.Sprintf("%d", tpuQUICPort),
			version,
		})
	})
}

func (s *Store) GetGossipIPs(ctx context.Context) ([]net.IP, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT DISTINCT gossip_ip FROM solana_gossip_nodes_current WHERE gossip_ip IS NOT NULL AND gossip_ip != ''`
	rows, err := conn.QueryContext(ctx, query)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating gossip IPs: %w", err)
	}

	return ips, nil
}

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

// FactTableConfigVoteAccountActivity returns the fact table config for vote account activity
func FactTableConfigVoteAccountActivity() duck.FactTableConfig {
	return duck.FactTableConfig{
		TableName:       "solana_vote_account_activity_raw",
		PartitionByTime: true,
		TimeColumn:      "time",
		Columns: []string{
			"time:TIMESTAMP",
			"vote_account_pubkey:VARCHAR",
			"node_identity_pubkey:VARCHAR",
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
		},
	}
}

type VoteAccountActivityEntry struct {
	Time                   time.Time
	VoteAccountPubkey      string
	NodeIdentityPubkey     string
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

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Build query with IN clause for all vote account pubkeys
	placeholders := make([]string, len(voteAccountPubkeys))
	args := make([]any, len(voteAccountPubkeys))
	for i, pubkey := range voteAccountPubkeys {
		placeholders[i] = "?"
		args[i] = pubkey
	}

	// Use ROW_NUMBER to get the latest row per vote_account_pubkey
	// This is more efficient than a subquery per account
	query := fmt.Sprintf(`SELECT vote_account_pubkey, credits_epoch, credits_epoch_credits
		FROM (
			SELECT
				vote_account_pubkey,
				credits_epoch,
				credits_epoch_credits,
				ROW_NUMBER() OVER (PARTITION BY vote_account_pubkey ORDER BY time DESC) AS rn
			FROM solana_vote_account_activity_raw
			WHERE vote_account_pubkey IN (%s)
		) ranked
		WHERE rn = 1`,
		strings.Join(placeholders, ","))

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous credits states: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*previousCreditsState)
	for rows.Next() {
		var pubkey string
		var epoch sql.NullInt64
		var epochCredits sql.NullInt64

		if err := rows.Scan(&pubkey, &epoch, &epochCredits); err != nil {
			return nil, fmt.Errorf("failed to scan previous credits state: %w", err)
		}

		if epoch.Valid && epochCredits.Valid {
			result[pubkey] = &previousCreditsState{
				Epoch:        int(epoch.Int64),
				EpochCredits: uint64(epochCredits.Int64),
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating previous credits states: %w", err)
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

	// Ensure table exists before querying it
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := FactTableConfigVoteAccountActivity()
	if err := duck.CreateFactTable(ctx, s.log, conn, cfg); err != nil {
		return fmt.Errorf("failed to create fact table: %w", err)
	}

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

	return duck.InsertFactsViaCSV(ctx, s.log, conn, cfg, len(entries), func(w *csv.Writer, i int) error {
		entry := entries[i]
		record := make([]string, 15)

		// time (required)
		record[0] = entry.Time.UTC().Format(time.RFC3339Nano)

		// vote_account_pubkey (required)
		record[1] = entry.VoteAccountPubkey

		// node_identity_pubkey (required)
		record[2] = entry.NodeIdentityPubkey

		// root_slot (required)
		record[3] = fmt.Sprintf("%d", entry.RootSlot)

		// last_vote_slot (required)
		record[4] = fmt.Sprintf("%d", entry.LastVoteSlot)

		// cluster_slot (required)
		record[5] = fmt.Sprintf("%d", entry.ClusterSlot)

		// is_delinquent (required)
		if entry.IsDelinquent {
			record[6] = "true"
		} else {
			record[6] = "false"
		}

		// epoch_credits_json (required)
		record[7] = entry.EpochCreditsJSON

		// credits_epoch (required)
		record[8] = fmt.Sprintf("%d", entry.CreditsEpoch)

		// credits_epoch_credits (required)
		record[9] = fmt.Sprintf("%d", entry.CreditsEpochCredits)

		// credits_delta (nullable)
		if entry.CreditsDelta != nil {
			record[10] = fmt.Sprintf("%d", *entry.CreditsDelta)
		} else {
			record[10] = ""
		}

		// activated_stake_lamports (optional)
		if entry.ActivatedStakeLamports != nil {
			record[11] = fmt.Sprintf("%d", *entry.ActivatedStakeLamports)
		} else {
			record[11] = ""
		}

		// activated_stake_sol (optional)
		if entry.ActivatedStakeSol != nil {
			record[12] = fmt.Sprintf("%f", *entry.ActivatedStakeSol)
		} else {
			record[12] = ""
		}

		// commission (optional)
		if entry.Commission != nil {
			record[13] = fmt.Sprintf("%d", *entry.Commission)
		} else {
			record[13] = ""
		}

		// collector_run_id (optional)
		record[14] = entry.CollectorRunID

		return w.Write(record)
	})
}

// FactTableConfigBlockProduction returns the fact table config for block production
func FactTableConfigBlockProduction() duck.FactTableConfig {
	return duck.FactTableConfig{
		TableName:       "solana_block_production_raw",
		PartitionByTime: true,
		TimeColumn:      "time",
		Columns: []string{
			"epoch:INTEGER",
			"time:TIMESTAMP",
			"leader_identity_pubkey:VARCHAR",
			"leader_slots_assigned_cum:BIGINT",
			"blocks_produced_cum:BIGINT",
		},
	}
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

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := FactTableConfigBlockProduction()
	if err := duck.CreateFactTable(ctx, s.log, conn, cfg); err != nil {
		return fmt.Errorf("failed to create fact table: %w", err)
	}

	return duck.InsertFactsViaCSV(ctx, s.log, conn, cfg, len(entries), func(w *csv.Writer, i int) error {
		entry := entries[i]
		return w.Write([]string{
			fmt.Sprintf("%d", entry.Epoch),
			entry.Time.UTC().Format(time.RFC3339Nano),
			entry.LeaderIdentityPubkey,
			fmt.Sprintf("%d", entry.LeaderSlotsAssignedCum),
			fmt.Sprintf("%d", entry.BlocksProducedCum),
		})
	})
}
