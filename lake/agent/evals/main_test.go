//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	neo4jtesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j/testing"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
)

var (
	sharedDB    *clickhousetesting.DB
	sharedNeo4j *neo4jtesting.DB
)

func TestMain(m *testing.M) {
	log := laketesting.NewLogger()
	ctx := context.Background()

	var err error
	sharedDB, err = clickhousetesting.NewDB(ctx, log, nil)
	if err != nil {
		log.Error("failed to create shared ClickHouse DB", "error", err)
		os.Exit(1)
	}

	// Initialize Neo4j (optional - tests can run without it)
	sharedNeo4j, err = neo4jtesting.NewDB(ctx, log, nil)
	if err != nil {
		log.Warn("failed to create shared Neo4j DB, graph tests will be skipped", "error", err)
		sharedNeo4j = nil
	}

	code := m.Run()

	sharedDB.Close()
	if sharedNeo4j != nil {
		sharedNeo4j.Close()
	}
	os.Exit(code)
}

func testClient(t *testing.T) clickhouse.Client {
	client := laketesting.NewClient(t, sharedDB)
	return client
}

// testClientInfo returns the test client along with database info for schema fetching.
func testClientInfo(t *testing.T) *laketesting.ClientInfo {
	return laketesting.NewClientWithInfo(t, sharedDB)
}

// testNeo4jClient returns a Neo4j client for testing, or nil if Neo4j is not available.
func testNeo4jClient(t *testing.T) neo4j.Client {
	if sharedNeo4j == nil {
		return nil
	}
	client, err := neo4jtesting.NewTestClient(t, sharedNeo4j)
	if err != nil {
		t.Logf("Warning: failed to create Neo4j client: %v", err)
		return nil
	}
	return client
}
