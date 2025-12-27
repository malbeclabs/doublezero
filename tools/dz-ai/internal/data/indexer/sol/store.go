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
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
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

func (s *Store) CreateTablesIfNotExists() error {
	tablePrefix := s.db.Catalog() + "." + s.db.Schema() + "."
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `solana_gossip_nodes (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			pubkey VARCHAR,
			gossip_ip VARCHAR,
			gossip_port INTEGER,
			tpuquic_ip VARCHAR,
			tpuquic_port INTEGER,
			version VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `solana_vote_accounts (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			vote_pubkey VARCHAR,
			node_pubkey VARCHAR,
			activated_stake_lamports BIGINT,
			epoch_vote_account BOOLEAN,
			commission_percentage INTEGER,
			last_vote_slot BIGINT,
			root_slot BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `solana_leader_schedule (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			node_pubkey VARCHAR,
			slots INTEGER[],
			slot_count INTEGER
		)`,
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	for _, schema := range schemas {
		if _, err := conn.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

type LeaderScheduleEntry struct {
	NodePubkey solana.PublicKey
	Slots      []uint64
}

func (s *Store) ReplaceLeaderSchedule(ctx context.Context, entries []LeaderScheduleEntry, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing leader schedule", "count", len(entries))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "solana_leader_schedule", len(entries), func(w *csv.Writer, i int) error {
		entry := entries[i]
		slotsStr := formatUint64Array(entry.Slots)
		return w.Write([]string{
			fetchedAt.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", currentEpoch),
			entry.NodePubkey.String(),
			slotsStr,
			fmt.Sprintf("%d", len(entry.Slots)),
		})
	}, []string{"snapshot_timestamp", "current_epoch", "node_pubkey"})
}

func (s *Store) ReplaceVoteAccounts(ctx context.Context, accounts []solanarpc.VoteAccountsResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing vote accounts", "count", len(accounts))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "solana_vote_accounts", len(accounts), func(w *csv.Writer, i int) error {
		account := accounts[i]
		epochVoteAccountStr := "false"
		if account.EpochVoteAccount {
			epochVoteAccountStr = "true"
		}
		return w.Write([]string{
			fetchedAt.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", currentEpoch),
			account.VotePubkey.String(),
			account.NodePubkey.String(),
			fmt.Sprintf("%d", account.ActivatedStake),
			epochVoteAccountStr,
			fmt.Sprintf("%d", account.Commission),
			fmt.Sprintf("%d", account.LastVote),
			fmt.Sprintf("%d", account.RootSlot),
		})
	}, []string{"snapshot_timestamp", "current_epoch", "vote_pubkey"})
}

func (s *Store) ReplaceGossipNodes(ctx context.Context, nodes []*solanarpc.GetClusterNodesResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing gossip nodes", "count", len(nodes))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "solana_gossip_nodes", len(nodes), func(w *csv.Writer, i int) error {
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
			fetchedAt.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", currentEpoch),
			node.Pubkey.String(),
			gossipIP,
			fmt.Sprintf("%d", gossipPort),
			tpuQUICIP,
			fmt.Sprintf("%d", tpuQUICPort),
			version,
		})
	}, []string{"snapshot_timestamp", "current_epoch", "pubkey"})
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
