package laketesting

import (
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	"github.com/stretchr/testify/require"
)

// ClientInfo holds a test client and its database name.
type ClientInfo struct {
	Client   clickhouse.Client
	Database string
}

func NewClient(t *testing.T, db *clickhousetesting.DB) clickhouse.Client {
	info := NewClientWithInfo(t, db)
	return info.Client
}

// NewClientWithInfo creates a test client and returns info including the database name.
func NewClientWithInfo(t *testing.T, db *clickhousetesting.DB) *ClientInfo {
	info, err := clickhousetesting.NewTestClientWithInfo(t, db)
	require.NoError(t, err)

	conn, err := info.Client.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()

	log := NewLogger()
	err = clickhouse.RunMigrations(t.Context(), log, conn)
	require.NoError(t, err)

	return &ClientInfo{
		Client:   info.Client,
		Database: info.Database,
	}
}
