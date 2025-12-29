package sol

import (
	"context"
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
		TrackIngestRuns:     true,
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
		PayloadColumns:      []string{"epoch:BIGINT", "node_pubkey:VARCHAR", "activated_stake_lamports:BIGINT", "epoch_vote_account:VARCHAR", "commission_percentage:BIGINT", "last_vote_slot:BIGINT", "root_slot:BIGINT"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     true,
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
			fmt.Sprintf("%d", account.LastVote),
			fmt.Sprintf("%d", account.RootSlot),
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
		TrackIngestRuns:     true,
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
