package sol

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
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
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS solana_gossip_nodes (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			pubkey VARCHAR PRIMARY KEY,
			gossip_ip VARCHAR,
			gossip_port INTEGER,
			tpuquic_ip VARCHAR,
			tpuquic_port INTEGER,
			version VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS solana_vote_accounts (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			vote_pubkey VARCHAR PRIMARY KEY,
			node_pubkey VARCHAR,
			activated_stake_lamports BIGINT,
			epoch_vote_account BOOLEAN,
			commission_percentage INTEGER,
			last_vote_slot BIGINT,
			root_slot BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS solana_leader_schedule (
			snapshot_timestamp TIMESTAMP,
			current_epoch INTEGER,
			node_pubkey VARCHAR PRIMARY KEY,
			slots INTEGER[],
			slot_count INTEGER
		)`,
	}

	for _, schema := range schemas {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

type LeaderScheduleEntry struct {
	NodePubkey solana.PublicKey
	Slots      []uint64
}

func (s *Store) ReplaceLeaderSchedule(entries []LeaderScheduleEntry, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing leader schedule", "count", len(entries))
	return s.replaceTable("solana_leader_schedule", "DELETE FROM solana_leader_schedule", "INSERT INTO solana_leader_schedule (snapshot_timestamp, current_epoch, node_pubkey, slots, slot_count) VALUES (?, ?, ?, ?, ?)", len(entries), func(stmt *sql.Stmt, i int) error {
		entry := entries[i]
		slotsStr := formatUint64Array(entry.Slots)
		_, err := stmt.Exec(fetchedAt, currentEpoch, entry.NodePubkey.String(), slotsStr, len(entry.Slots))
		return err
	})
}

func (s *Store) ReplaceVoteAccounts(accounts []solanarpc.VoteAccountsResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing vote accounts", "count", len(accounts))
	return s.replaceTable("solana_vote_accounts", "DELETE FROM solana_vote_accounts", "INSERT INTO solana_vote_accounts (snapshot_timestamp, current_epoch, vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage, last_vote_slot, root_slot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", len(accounts), func(stmt *sql.Stmt, i int) error {
		account := accounts[i]
		_, err := stmt.Exec(fetchedAt, currentEpoch, account.VotePubkey.String(), account.NodePubkey.String(), account.ActivatedStake, account.EpochVoteAccount, account.Commission, account.LastVote, account.RootSlot)
		return err
	})
}

func (s *Store) ReplaceGossipNodes(nodes []*solanarpc.GetClusterNodesResult, fetchedAt time.Time, currentEpoch uint64) error {
	s.log.Debug("solana/store: replacing gossip nodes", "count", len(nodes))
	return s.replaceTable("solana_gossip_nodes", "DELETE FROM solana_gossip_nodes", "INSERT INTO solana_gossip_nodes (snapshot_timestamp, current_epoch, pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", len(nodes), func(stmt *sql.Stmt, i int) error {
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
		_, err := stmt.Exec(fetchedAt, currentEpoch, node.Pubkey.String(), gossipIP, gossipPort, tpuQUICIP, tpuQUICPort, node.Version)
		return err
	})
}

func (s *Store) replaceTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	s.log.Info("solana: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		s.log.Info("solana: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	s.log.Debug("solana: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	s.log.Debug("solana: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		s.log.Debug("solana: table refreshed (empty)", "table", tableName)
		return nil
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for %s: %w", tableName, err)
	}
	defer stmt.Close()

	logInterval := min(max(count/10, 1000), 100000)

	for i := range count {
		if err := insertFn(stmt, i); err != nil {
			s.log.Error("failed to insert row", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
		if (i+1)%logInterval == 0 || i == count-1 {
			s.log.Debug("insert progress", "table", tableName, "inserted", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	commitStart := time.Now()
	s.log.Info("solana: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		s.log.Error("solana: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	s.log.Info("solana: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	s.log.Debug("solana: table refreshed", "table", tableName, "rows", count)
	return nil
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

