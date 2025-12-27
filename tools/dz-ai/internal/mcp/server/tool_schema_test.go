package server

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestAI_MCP_Server_ToolSchema_Register(t *testing.T) {
	t.Parallel()

	t.Run("registers tool successfully", func(t *testing.T) {
		t.Parallel()

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "test_table",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
					},
				},
			},
		}

		server := mcp.NewServer(&mcp.Implementation{
			Name:    "Test Server",
			Version: "1.0.0",
		}, nil)

		err := RegisterSchemaTool(testLogger(t), server, schema)
		require.NoError(t, err)
	})
}

func TestAI_MCP_Server_ToolSchema_RegisterWithNilSchema(t *testing.T) {
	t.Parallel()

	t.Run("returns error when schema is nil", func(t *testing.T) {
		t.Parallel()

		server := mcp.NewServer(&mcp.Implementation{
			Name:    "Test Server",
			Version: "1.0.0",
		}, nil)

		err := RegisterSchemaTool(testLogger(t), server, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "schema is required")
	})
}
