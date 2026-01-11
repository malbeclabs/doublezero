package laketesting

import (
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	"github.com/stretchr/testify/require"
)

func NewClient(t *testing.T, db *clickhousetesting.DB) clickhouse.Client {
	client, err := clickhousetesting.NewTestClient(t, db)
	require.NoError(t, err)

	conn, err := client.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()

	log := NewLogger()
	err = clickhouse.RunMigrations(t.Context(), log, conn)
	require.NoError(t, err)

	return client
}
