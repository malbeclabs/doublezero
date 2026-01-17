package graph

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	neo4jtesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j/testing"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

var (
	sharedDB      *clickhousetesting.DB
	sharedNeo4jDB *neo4jtesting.DB
)

func TestMain(m *testing.M) {
	log := laketesting.NewLogger()
	var err error

	// Create shared ClickHouse DB
	sharedDB, err = clickhousetesting.NewDB(context.Background(), log, nil)
	if err != nil {
		log.Error("failed to create shared ClickHouse DB", "error", err)
		os.Exit(1)
	}

	// Create shared Neo4j DB
	sharedNeo4jDB, err = neo4jtesting.NewDB(context.Background(), log, nil)
	if err != nil {
		log.Error("failed to create shared Neo4j DB", "error", err)
		sharedDB.Close()
		os.Exit(1)
	}

	code := m.Run()
	sharedDB.Close()
	sharedNeo4jDB.Close()
	os.Exit(code)
}

func testClickHouseClient(t *testing.T) clickhouse.Client {
	client := laketesting.NewClient(t, sharedDB)
	return client
}

func testNeo4jClient(t *testing.T) neo4j.Client {
	client, err := neo4jtesting.NewTestClient(t, sharedNeo4jDB)
	require.NoError(t, err)
	return client
}
