package sol

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SchemaRequest struct{}

type SchemaResponse struct {
	Tables []TableSchema `json:"tables"`
}

type TableSchema struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

func (t *Tools) registerSchema(server *mcp.Server) error {
	req, err := jsonschema.For[SchemaRequest](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema input schema: %w", err)
	}

	res, err := jsonschema.For[SchemaResponse](nil)
	if err != nil {
		return fmt.Errorf("failed to create schema output schema: %w", err)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:         "solana-schema",
		Description:  "List all available Solana tables and their schemas. Only use this dataset when questions are specifically about Solana validators or the Solana blockchain. Start with doublezero-schema for general network questions, or doublezero-telemetry-schema for performance metrics.",
		InputSchema:  req,
		OutputSchema: res,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, req SchemaRequest) (*mcp.CallToolResult, SchemaResponse, error) {
		return nil, getSchema(), nil
	})

	return nil
}

func getSchema() SchemaResponse {
	tables := make([]TableSchema, 0, len(tableSchemas))
	for _, schema := range tableSchemas {
		tables = append(tables, schema)
	}
	return SchemaResponse{Tables: tables}
}

func (t *Tools) validateSchema() error {
	// Build list of expected table names from in-code schema
	expectedTables := make([]string, 0, len(tableSchemas))
	for _, schema := range tableSchemas {
		expectedTables = append(expectedTables, schema.Name)
	}

	// Build query with explicit table names
	tableNames := make([]string, len(expectedTables))
	for i, name := range expectedTables {
		tableNames[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(name, "'", "''"))
	}
	query := fmt.Sprintf(`
		SELECT
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_schema = 'main'
			AND table_name IN (%s)
		ORDER BY table_name, ordinal_position
	`, strings.Join(tableNames, ", "))

	rows, err := t.db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query schema: %w", err)
	}
	defer rows.Close()

	tableColumnMap := make(map[string]map[string]string)
	for rows.Next() {
		var tableName, columnName, dataType string
		if err := rows.Scan(&tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan schema row: %w", err)
		}
		if tableColumnMap[tableName] == nil {
			tableColumnMap[tableName] = make(map[string]string)
		}
		tableColumnMap[tableName][columnName] = dataType
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating schema rows: %w", err)
	}

	// Build map of in-code schemas
	inCodeSchemas := make(map[string]map[string]ColumnInfo)
	for _, schema := range tableSchemas {
		inCodeSchemas[schema.Name] = make(map[string]ColumnInfo)
		for _, col := range schema.Columns {
			inCodeSchemas[schema.Name][col.Name] = col
		}
	}

	var missing []string
	for tableName, dbColumns := range tableColumnMap {
		inCodeTable, exists := inCodeSchemas[tableName]
		if !exists {
			missing = append(missing, fmt.Sprintf("table %s: missing from in-code schema", tableName))
			continue
		}

		for colName := range dbColumns {
			inCodeCol, exists := inCodeTable[colName]
			if !exists {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing from in-code schema", tableName, colName))
				continue
			}
			if inCodeCol.Description == "" {
				missing = append(missing, fmt.Sprintf("table %s, column %s: missing description", tableName, colName))
			}
		}
	}

	// Check for tables in in-code schema that don't exist in database
	for tableName := range inCodeSchemas {
		if _, exists := tableColumnMap[tableName]; !exists {
			missing = append(missing, fmt.Sprintf("table %s: in-code schema but not in database", tableName))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("schema validation failed:\n  %s", strings.Join(missing, "\n  "))
	}

	return nil
}

var tableSchemas = []TableSchema{
	{
		Name:        "solana_gossip_nodes",
		Description: "Solana gossip nodes. These are not validators, but are part of the solana gossip network. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the gossip node.",
		Columns: []ColumnInfo{
			{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
			{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
			{Name: "pubkey", Type: "VARCHAR", Description: "Public key of the gossip node. join to solana_vote_accounts.node_pubkey."},
			{Name: "gossip_ip", Type: "VARCHAR", Description: "Gossip IP address. Join to dz_users.dz_ip to get the user associated with the solana gossip node."},
			{Name: "gossip_port", Type: "INTEGER", Description: "Gossip port"},
			{Name: "tpuquic_ip", Type: "VARCHAR", Description: "TPU-QUIC IP address"},
			{Name: "tpuquic_port", Type: "INTEGER", Description: "TPU-QUIC port"},
			{Name: "version", Type: "VARCHAR", Description: "The Solana client version running on the gossip node."},
		},
	},
	{
		Name:        "solana_vote_accounts",
		Description: "Solana vote accounts. These are solana validators. Join to solana_gossip_nodes.pubkey to get the gossip node associated with the vote account.",
		Columns: []ColumnInfo{
			{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
			{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
			{Name: "vote_pubkey", Type: "VARCHAR", Description: "Vote public key The unique identifier for a validator (vote account)."},
			{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key. Join to solana_gossip_nodes.pubkey to get the gossip node associated with the vote account."},
			{Name: "activated_stake_lamports", Type: "BIGINT", Description: "Activated stake (lamports). There are 1_000_000_000 lamports in 1 SOL."},
			{Name: "epoch_vote_account", Type: "BOOLEAN", Description: "Whether the vote account is staked for this epoch."},
			{Name: "commission_percentage", Type: "INTEGER", Description: "Commission percentage. The percentage of rewards payout owed to the vote account."},
			{Name: "last_vote_slot", Type: "BIGINT", Description: "Last vote slot. The slot of the last vote cast by the vote account."},
			{Name: "root_slot", Type: "BIGINT", Description: "Root slot. The slot of the last root block processed by the vote account."},
		},
	},
	{
		Name:        "solana_leader_schedule",
		Description: "Solana leader schedule. These are the validators that are leaders for given slots in an epoch. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the leader schedule.",
		Columns: []ColumnInfo{
			{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
			{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
			{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the leader schedule."},
			{Name: "slots", Type: "INTEGER[]", Description: "The slots that the node is leader for."},
			{Name: "slot_count", Type: "INTEGER", Description: "The number of slots that the node is leader for. Can be used to calcluate how often the node is leader."},
		},
	},
}
