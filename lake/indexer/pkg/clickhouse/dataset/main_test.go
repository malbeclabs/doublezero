package dataset

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	"github.com/stretchr/testify/require"
)

var (
	sharedDB *clickhousetesting.DB
)

func TestMain(m *testing.M) {
	log := testLogger()
	var err error
	sharedDB, err = clickhousetesting.NewDB(context.Background(), log, nil)
	if err != nil {
		log.Error("failed to create shared DB", "error", err)
		os.Exit(1)
	}
	code := m.Run()
	sharedDB.Close()
	os.Exit(code)
}

func testConn(t *testing.T) clickhouse.Connection {
	conn, err := clickhousetesting.NewTestConn(t, sharedDB)
	require.NoError(t, err)
	return conn
}
