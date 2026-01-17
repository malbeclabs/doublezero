package neo4j_test

import (
	"context"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	neo4jtesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j/testing"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

var sharedDB *neo4jtesting.DB

func TestMain(m *testing.M) {
	log := laketesting.NewLogger()
	var err error
	sharedDB, err = neo4jtesting.NewDB(context.Background(), log, nil)
	if err != nil {
		log.Error("failed to create shared Neo4j DB", "error", err)
		os.Exit(1)
	}
	code := m.Run()
	sharedDB.Close()
	os.Exit(code)
}

func testClient(t *testing.T) neo4j.Client {
	client, err := neo4jtesting.NewTestClient(t, sharedDB)
	require.NoError(t, err)
	return client
}

func TestClient_Session(t *testing.T) {
	client := testClient(t)
	ctx := t.Context()

	session, err := client.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Create a node
	res, err := session.Run(ctx, "CREATE (n:TestNode {name: $name}) RETURN n.name AS name", map[string]any{
		"name": "test",
	})
	require.NoError(t, err)

	record, err := res.Single(ctx)
	require.NoError(t, err)

	name, _ := record.Get("name")
	require.Equal(t, "test", name)
}

func TestClient_ExecuteWrite(t *testing.T) {
	client := testClient(t)
	ctx := t.Context()

	session, err := client.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Execute a write transaction
	result, err := session.ExecuteWrite(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "CREATE (n:TestNode {name: $name}) RETURN n.name AS name", map[string]any{
			"name": "write_test",
		})
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		name, _ := record.Get("name")
		return name, nil
	})
	require.NoError(t, err)
	require.Equal(t, "write_test", result)
}

func TestClient_ExecuteRead(t *testing.T) {
	client := testClient(t)
	ctx := t.Context()

	session, err := client.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// First create a node
	res, err := session.Run(ctx, "CREATE (n:TestNode {name: $name})", map[string]any{
		"name": "read_test",
	})
	require.NoError(t, err)
	_, err = res.Consume(ctx)
	require.NoError(t, err)

	// Execute a read transaction
	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "MATCH (n:TestNode {name: $name}) RETURN n.name AS name", map[string]any{
			"name": "read_test",
		})
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		name, _ := record.Get("name")
		return name, nil
	})
	require.NoError(t, err)
	require.Equal(t, "read_test", result)
}

func TestInitializeSchema(t *testing.T) {
	client := testClient(t)
	ctx := t.Context()

	// Schema should already be initialized by NewTestClient
	// Verify by querying constraints
	session, err := client.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	res, err := session.Run(ctx, "SHOW CONSTRAINTS", nil)
	require.NoError(t, err)

	records, err := res.Collect(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 5, "expected at least 5 constraints")
}
