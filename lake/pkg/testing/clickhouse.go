package laketesting

import (
	"context"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/pkg/clickhouse/testing"
	"github.com/stretchr/testify/require"
)

func NewDB(t *testing.T) clickhouse.DB {
	db := clickhousetesting.NewDefaultDB(t)
	log := NewLogger(t)

	conn := db.Conn()
	t.Cleanup(func() {
		conn.Close()
	})
	err := clickhouse.RunMigrations(context.Background(), log, conn)
	require.NoError(t, err)

	return db.DB
}
