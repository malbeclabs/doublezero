package sol

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	"github.com/stretchr/testify/require"
)

type failingDB struct{}

func (f *failingDB) Exec(query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) Query(query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) QueryRow(query string, args ...any) *sql.Row {
	return &sql.Row{}
}
func (f *failingDB) Begin() (*sql.Tx, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) Close() error {
	return nil
}
func (f *failingDB) ReplaceTable(tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	return errors.New("database error")
}

func TestMCP_Solana_Store_NewStore(t *testing.T) {
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

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

func TestMCP_Solana_Store_CreateTablesIfNotExists(t *testing.T) {
	t.Parallel()

	t.Run("creates all tables", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Verify tables exist by querying them
		tables := []string{"solana_gossip_nodes", "solana_vote_accounts", "solana_leader_schedule"}
		for _, table := range tables {
			var count int
			err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
			require.NoError(t, err, "table %s should exist", table)
		}
	})

	t.Run("returns error when database fails", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     &failingDB{},
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create table")
	})
}

func TestMCP_Solana_Store_ReplaceLeaderSchedule(t *testing.T) {
	t.Parallel()

	t.Run("saves leader schedule to database", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
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

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_leader_schedule").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var nodePubkey string
		var slotCount int
		var slotsArray []interface{}
		var currentEpochDB int64
		err = db.QueryRow("SELECT node_pubkey, slots, slot_count, current_epoch FROM solana_leader_schedule LIMIT 1").Scan(&nodePubkey, &slotsArray, &slotCount, &currentEpochDB)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), nodePubkey)
		require.Equal(t, 3, slotCount)
		require.Equal(t, int64(100), currentEpochDB)
		require.Len(t, slotsArray, 3)
		// DuckDB returns integers in arrays as int32
		require.Equal(t, int32(100), slotsArray[0])
		require.Equal(t, int32(200), slotsArray[1])
		require.Equal(t, int32(300), slotsArray[2])
	})
}

func TestMCP_Solana_Store_ReplaceVoteAccounts(t *testing.T) {
	t.Parallel()

	t.Run("saves vote accounts to database", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
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

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_vote_accounts").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var votePubkey, nodePubkey string
		var activatedStake, lastVoteSlot, rootSlot int64
		var epochVoteAccount bool
		var commission int
		var currentEpochDB int64
		err = db.QueryRow("SELECT vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage, last_vote_slot, root_slot, current_epoch FROM solana_vote_accounts LIMIT 1").Scan(&votePubkey, &nodePubkey, &activatedStake, &epochVoteAccount, &commission, &lastVoteSlot, &rootSlot, &currentEpochDB)
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

func TestMCP_Solana_Store_ReplaceGossipNodes(t *testing.T) {
	t.Parallel()

	t.Run("saves gossip nodes to database", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
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

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_gossip_nodes").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pubkey, gossipIP, tpuQUICIP, version string
		var gossipPort, tpuQUICPort int
		var currentEpochDB int64
		err = db.QueryRow("SELECT pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version, current_epoch FROM solana_gossip_nodes LIMIT 1").Scan(&pubkey, &gossipIP, &gossipPort, &tpuQUICIP, &tpuQUICPort, &version, &currentEpochDB)
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
