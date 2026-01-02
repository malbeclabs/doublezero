package sol

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

type failingDB struct{}

func (f *failingDB) Close() error {
	return nil
}

func (f *failingDB) Catalog() string {
	return "main"
}

func (f *failingDB) Schema() string {
	return "default"
}

func (f *failingDB) Conn(ctx context.Context) (duck.Connection, error) {
	return &failingDBConn{db: f}, nil
}

type failingDBConn struct {
	db *failingDB
}

func (f *failingDBConn) DB() duck.DB {
	if f.db == nil {
		return &failingDB{}
	}
	return f.db
}

func (f *failingDBConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return &sql.Row{}
}

func (f *failingDBConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) Close() error {
	return nil
}
func (f *failingDB) ReplaceTable(tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	return errors.New("database error")
}

func TestLake_Solana_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				DB: &failingDB{},
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing db", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "db is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}


func TestLake_Solana_Store_ReplaceLeaderSchedule(t *testing.T) {
	t.Parallel()

	t.Run("saves leader schedule to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)


		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		slots := []uint64{100, 200, 300}
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		entries := []LeaderScheduleEntry{
			{
				NodePubkey: nodePK,
				Slots:      slots,
			},
		}

		err = store.ReplaceLeaderSchedule(context.Background(), entries, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_leader_schedule_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var nodePubkey string
		var slotCount int
		var slotsStr string
		var currentEpochDB int64
		err = conn.QueryRowContext(ctx, "SELECT node_pubkey, slots, slot_count, epoch FROM solana_leader_schedule_current LIMIT 1").Scan(&nodePubkey, &slotsStr, &slotCount, &currentEpochDB)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), nodePubkey)
		require.Equal(t, 3, slotCount)
		require.Equal(t, int64(100), currentEpochDB)
		// Slots are stored as VARCHAR in SCD2 format: "[100,200,300]"
		require.Equal(t, "[100,200,300]", slotsStr)
	})
}

func TestLake_Solana_Store_ReplaceVoteAccounts(t *testing.T) {
	t.Parallel()

	t.Run("saves vote accounts to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)


		votePK := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")
		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		accounts := []solanarpc.VoteAccountsResult{
			{
				VotePubkey:       votePK,
				NodePubkey:       nodePK,
				ActivatedStake:   1000000000,
				EpochVoteAccount: true,
				Commission:       5,
				LastVote:         5000,
				RootSlot:         4500,
			},
		}

		err = store.ReplaceVoteAccounts(context.Background(), accounts, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_vote_accounts_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var votePubkey, nodePubkey string
		var activatedStake, lastVoteSlot, rootSlot int64
		var epochVoteAccount bool
		var commission int
		var currentEpochDB int64
		err = conn.QueryRowContext(ctx, "SELECT vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage, last_vote_slot, root_slot, epoch FROM solana_vote_accounts_current LIMIT 1").Scan(&votePubkey, &nodePubkey, &activatedStake, &epochVoteAccount, &commission, &lastVoteSlot, &rootSlot, &currentEpochDB)
		require.NoError(t, err)
		require.Equal(t, votePK.String(), votePubkey)
		require.Equal(t, nodePK.String(), nodePubkey)
		require.Equal(t, int64(1000000000), activatedStake)
		require.True(t, epochVoteAccount)
		require.Equal(t, 5, commission)
		require.Equal(t, int64(5000), lastVoteSlot)
		require.Equal(t, int64(4500), rootSlot)
		require.Equal(t, int64(100), currentEpochDB)
	})
}

func TestLake_Solana_Store_ReplaceGossipNodes(t *testing.T) {
	t.Parallel()

	t.Run("saves gossip nodes to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)


		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		gossipAddr := "192.168.1.1:8001"
		tpuQUICAddr := "192.168.1.1:8002"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK,
				Gossip:  &gossipAddr,
				TPUQUIC: &tpuQUICAddr,
				Version: &nodeVersion,
			},
		}

		err = store.ReplaceGossipNodes(context.Background(), nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_gossip_nodes_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pubkey, gossipIP, tpuQUICIP, version string
		var gossipPort, tpuQUICPort int
		var currentEpochDB int64
		err = conn.QueryRowContext(ctx, "SELECT pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version, epoch FROM solana_gossip_nodes_current LIMIT 1").Scan(&pubkey, &gossipIP, &gossipPort, &tpuQUICIP, &tpuQUICPort, &version, &currentEpochDB)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), pubkey)
		require.Equal(t, "192.168.1.1", gossipIP)
		require.Equal(t, 8001, gossipPort)
		require.Equal(t, "192.168.1.1", tpuQUICIP)
		require.Equal(t, 8002, tpuQUICPort)
		require.Equal(t, "1.0.0", version)
		require.Equal(t, int64(100), currentEpochDB)
	})
}

func TestLake_Solana_Store_GetGossipIPs(t *testing.T) {
	t.Parallel()

	t.Run("reads gossip IPs from database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		nodePK1 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		nodePK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		gossipAddr1 := "192.168.1.1:8001"
		gossipAddr2 := "192.168.1.2:8001"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK1,
				Gossip:  &gossipAddr1,
				Version: &nodeVersion,
			},
			{
				Pubkey:  nodePK2,
				Gossip:  &gossipAddr2,
				Version: &nodeVersion,
			},
		}

		err = store.ReplaceGossipNodes(context.Background(), nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		ips, err := store.GetGossipIPs(ctx)
		require.NoError(t, err)
		require.Len(t, ips, 2)

		// Check that IPs are parsed correctly
		ipStrings := make([]string, len(ips))
		for i, ip := range ips {
			ipStrings[i] = ip.String()
		}
		require.Contains(t, ipStrings, "192.168.1.1")
		require.Contains(t, ipStrings, "192.168.1.2")
	})

	t.Run("returns distinct IPs only", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		nodePK1 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		nodePK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		gossipAddr := "192.168.1.1:8001"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK1,
				Gossip:  &gossipAddr,
				Version: &nodeVersion,
			},
			{
				Pubkey:  nodePK2,
				Gossip:  &gossipAddr, // Same IP
				Version: &nodeVersion,
			},
		}

		err = store.ReplaceGossipNodes(context.Background(), nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		ips, err := store.GetGossipIPs(ctx)
		require.NoError(t, err)
		require.Len(t, ips, 1) // Should be deduplicated
		require.Equal(t, net.ParseIP("192.168.1.1"), ips[0])
	})

	t.Run("filters out NULL and empty gossip IPs", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		nodePK1 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		nodePK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		gossipAddr1 := "192.168.1.1:8001"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK1,
				Gossip:  &gossipAddr1,
				Version: &nodeVersion,
			},
			{
				Pubkey:  nodePK2,
				Gossip:  nil, // No gossip address
				Version: &nodeVersion,
			},
		}

		err = store.ReplaceGossipNodes(context.Background(), nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		ctx := context.Background()
		ips, err := store.GetGossipIPs(ctx)
		require.NoError(t, err)
		require.Len(t, ips, 1) // Only the node with gossip IP
		require.Equal(t, net.ParseIP("192.168.1.1"), ips[0])
	})

	t.Run("returns empty slice when no gossip IPs", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		// Create empty table so query doesn't fail
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey VARCHAR,
			epoch BIGINT,
			gossip_ip VARCHAR,
			gossip_port INTEGER,
			tpuquic_ip VARCHAR,
			tpuquic_port INTEGER,
			version VARCHAR,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		ips, err := store.GetGossipIPs(ctx)
		require.NoError(t, err)
		require.Empty(t, ips)
	})
}
