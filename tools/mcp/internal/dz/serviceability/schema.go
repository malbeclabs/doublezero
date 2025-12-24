package dzsvc

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
		Name:         "doublezero-schema",
		Description:  "List all available DoubleZero serviceability tables and their schemas. This is the PRIMARY dataset - start here for questions about network structure, devices, links, contributors, users, or metro locations. Use doublezero-telemetry-schema for performance/latency metrics, or solana-schema for Solana validator data. For more information about DoubleZero, see https://doublezero.xyz",
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
		Name:        "dz_contributors",
		Description: "Contributors in the DoubleZero network. Each contributor operates one or more devices and links. You can join using contributor code from devices and links.",
		Columns: []ColumnInfo{
			{Name: "pk", Type: "VARCHAR", Description: "Primary key. Join target for devices.contributor_pk, links.contributor_pk"},
			{Name: "code", Type: "VARCHAR", Description: "Contributor code. Short human readable identifier for the contributor."},
			{Name: "name", Type: "VARCHAR", Description: "Contributor name. Full human readable name for the contributor."},
		},
	},
	{
		Name:        "dz_devices",
		Description: "Network devices. Join to metros via metro_pk. Each device is operated by a contributor.",
		Columns: []ColumnInfo{
			{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
			{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, soft-drained, hard-drained"},
			{Name: "device_type", Type: "VARCHAR", Description: "hybrid, transit, edge"},
			{Name: "code", Type: "VARCHAR", Description: "Device code. Human readable identifier for the device (e.g., la2-dz01, ny5-dz01)"},
			{Name: "public_ip", Type: "VARCHAR", Description: "Public IP address"},
			{Name: "contributor_pk", Type: "VARCHAR", Description: "Join to contributors.pk"},
			{Name: "metro_pk", Type: "VARCHAR", Description: "Join to metros.pk (metro location, from exchange_pubkey)"},
		},
	},
	{
		Name:        "dz_metros",
		Description: "Metro areas (also called exchanges). Join target for devices via metro_pk",
		Columns: []ColumnInfo{
			{Name: "pk", Type: "VARCHAR", Description: "Primary key. Join target for devices.metro_pk"},
			{Name: "code", Type: "VARCHAR", Description: "Metro code (e.g., nyc, lon, fra)"},
			{Name: "name", Type: "VARCHAR", Description: "Metro name (e.g., New York, London, Frankfurt)"},
			{Name: "longitude", Type: "DOUBLE", Description: "Longitude"},
			{Name: "latitude", Type: "DOUBLE", Description: "Latitude"},
		},
	},
	{
		Name:        "dz_links",
		Description: "Network links connecting 2 devices. Join to devices via side_a_pk or side_z_pk.",
		Columns: []ColumnInfo{
			{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
			{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, requested, hard-drained, soft-drained"},
			{Name: "code", Type: "VARCHAR", Description: "Link code. Human readable identifier for the link (e.g., la2-dz01:ny5-dz01)"},
			{Name: "tunnel_net", Type: "VARCHAR", Description: "Tunnel network CIDR (e.g., 172.16.0.0/31)"},
			{Name: "contributor_pk", Type: "VARCHAR", Description: "Join to contributors.pk"},
			{Name: "side_a_pk", Type: "VARCHAR", Description: "Join to devices.pk (device on the A side of the link)"},
			{Name: "side_z_pk", Type: "VARCHAR", Description: "Join to devices.pk (device on the Z side of the link)"},
			{Name: "side_a_iface_name", Type: "VARCHAR", Description: "Interface name on side A"},
			{Name: "side_z_iface_name", Type: "VARCHAR", Description: "Interface name on side Z"},
			{Name: "link_type", Type: "VARCHAR", Description: "WAN or DZX"},
			{Name: "delay_ns", Type: "BIGINT", Description: "Committed delay (nanoseconds)"},
			{Name: "jitter_ns", Type: "BIGINT", Description: "Committed jitter (nanoseconds)"},
		},
	},
	{
		Name:        "dz_users",
		Description: "Users connected to the DoubleZero network via devices. Join to devices via dz_users.device_pk. Some users are Solana validators, but not all users are Solana validators. Join to solana_gossip_nodes.gossip_ip via dz_users.dz_ip to get the gossip node associated with the user.",
		Columns: []ColumnInfo{
			{Name: "pk", Type: "VARCHAR", Description: "Primary key"},
			{Name: "owner_pk", Type: "VARCHAR", Description: "Owner public key"},
			{Name: "status", Type: "VARCHAR", Description: "pending, activated, suspended, deleted, rejected, pending_ban, banned, updating"},
			{Name: "kind", Type: "VARCHAR", Description: "Connection type: ibrl, ibrl_with_allocated_ip, edge_filtering, multicast"},
			{Name: "client_ip", Type: "VARCHAR", Description: "Client IP address"},
			{Name: "dz_ip", Type: "VARCHAR", Description: "DoubleZero IP address. Join to solana_gossip_nodes.gossip_ip to get the gossip node associated with the user"},
			{Name: "device_pk", Type: "VARCHAR", Description: "Join to devices.pk (device that the user is connected to)"},
		},
	},
}
