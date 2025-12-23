package sol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
)

type SolanaRPC interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

type ViewConfig struct {
	Logger *slog.Logger
	Clock  clockwork.Clock
	RPC    SolanaRPC
	DB     duck.DB

	RefreshInterval time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.RPC == nil {
		return errors.New("rpc is required")
	}
	if cfg.DB == nil {
		return errors.New("database is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}

	// Optional with default
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log *slog.Logger
	cfg ViewConfig
	db  duck.DB

	fetchedAt time.Time

	readyOnce sync.Once
	readyCh   chan struct{}
}

func NewView(
	cfg ViewConfig,
) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}
	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		db:      cfg.DB,
		readyCh: make(chan struct{}),
	}
	if err := v.initDB(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}
	return v, nil
}

func (v *View) Close() error {
	return nil
}

func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for serviceability view: %w", ctx.Err())
	}
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("solana: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("solana: initial refresh failed", "error", err)
		}
		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				if err := v.Refresh(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					v.log.Error("solana: refresh failed", "error", err)
				}
			}
		}
	}()
}

type leaderScheduleEntry struct {
	NodePubkey solana.PublicKey
	Slots      []uint64
}

func (v *View) Refresh(ctx context.Context) error {
	refreshStart := time.Now()
	v.log.Info("solana: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("solana: refresh completed", "duration", duration.String())
	}()

	v.log.Debug("solana: starting refresh")

	fetchedAt := time.Now().UTC()

	epochInfo, err := v.cfg.RPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	leaderSchedule, err := v.cfg.RPC.GetLeaderSchedule(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leader schedule: %w", err)
	}
	leaderScheduleEntries := make([]leaderScheduleEntry, 0, len(leaderSchedule))
	for pk, slots := range leaderSchedule {
		leaderScheduleEntries = append(leaderScheduleEntries, leaderScheduleEntry{
			NodePubkey: pk,
			Slots:      slots,
		})
	}

	voteAccounts, err := v.cfg.RPC.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentFinalized,
	})
	if err != nil {
		return fmt.Errorf("failed to get vote accounts: %w", err)
	}

	clusterNodes, err := v.cfg.RPC.GetClusterNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	v.log.Debug("solana: refreshing leader schedule", "count", len(leaderScheduleEntries))
	if err := v.refreshTable("solana_leader_schedule", "DELETE FROM solana_leader_schedule", "INSERT INTO solana_leader_schedule (snapshot_timestamp, current_epoch, node_pubkey, slots, slot_count) VALUES (?, ?, ?, ?, ?)", len(leaderScheduleEntries), func(stmt *sql.Stmt, i int) error {
		entry := leaderScheduleEntries[i]
		slotsStr := formatUint64Array(entry.Slots)
		_, err := stmt.Exec(fetchedAt, currentEpoch, entry.NodePubkey.String(), slotsStr, len(entry.Slots))
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh leader schedule: %w", err)
	}

	v.log.Debug("solana: refreshing vote accounts", "count", len(voteAccounts.Current))
	if err := v.refreshTable("solana_vote_accounts", "DELETE FROM solana_vote_accounts", "INSERT INTO solana_vote_accounts (snapshot_timestamp, current_epoch, vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage, last_vote_slot, root_slot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", len(voteAccounts.Current), func(stmt *sql.Stmt, i int) error {
		account := voteAccounts.Current[i]
		_, err := stmt.Exec(fetchedAt, currentEpoch, account.VotePubkey.String(), account.NodePubkey.String(), account.ActivatedStake, account.EpochVoteAccount, account.Commission, account.LastVote, account.RootSlot)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh vote accounts: %w", err)
	}

	v.log.Debug("solana: refreshing cluster nodes", "count", len(clusterNodes))
	if err := v.refreshTable("solana_gossip_nodes", "DELETE FROM solana_gossip_nodes", "INSERT INTO solana_gossip_nodes (snapshot_timestamp, current_epoch, pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", len(clusterNodes), func(stmt *sql.Stmt, i int) error {
		node := clusterNodes[i]
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
		_, err = stmt.Exec(fetchedAt, currentEpoch, node.Pubkey.String(), gossipIP, gossipPort, tpuQUICIP, tpuQUICPort, node.Version)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh cluster nodes: %w", err)
	}

	v.fetchedAt = fetchedAt
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("solana: view is now ready")
	})

	v.log.Debug("solana: refresh completed", "fetched_at", fetchedAt)
	return nil
}

func (v *View) refreshTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	v.log.Info("solana: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		v.log.Info("solana: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	v.log.Debug("solana: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	v.log.Debug("solana: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		v.log.Debug("solana: table refreshed (empty)", "table", tableName)
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
			v.log.Error("failed to insert row", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
		if (i+1)%logInterval == 0 || i == count-1 {
			v.log.Debug("insert progress", "table", tableName, "inserted", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	commitStart := time.Now()
	v.log.Info("solana: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		v.log.Error("solana: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	v.log.Info("solana: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	v.log.Debug("solana: table refreshed", "table", tableName, "rows", count)
	return nil
}

func (v *View) initDB() error {
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
		if _, err := v.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

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
