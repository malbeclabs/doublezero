package dztelem

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
		Name:         "doublezero-telemetry-schema",
		Description:  "List all available DoubleZero telemetry tables and their schemas. Use this dataset for questions about performance, latency, statistics, metrics, measurements, or historical performance data (RTT, jitter, packet loss, circuit performance). Start with doublezero-schema for network structure questions. For more information about DoubleZero, see https://doublezero.xyz",
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
		Name:        "dz_device_link_circuits",
		Description: "Device-to-device circuits. Join: dz_device_link_latency_samples.circuit_code → dz_device_link_circuits.code → dz_devices → dz_metros. Metro pair: origin_metro.code || ' → ' || target_metro.code",
		Columns: []ColumnInfo{
			{Name: "code", Type: "VARCHAR", Description: "Circuit code (format: origin → target (link_suffix)). Join key from dz_device_link_latency_samples.circuit_code"},
			{Name: "origin_device_pk", Type: "VARCHAR", Description: "Join to devices.pk → devices.metro_pk → metros (origin metro)"},
			{Name: "target_device_pk", Type: "VARCHAR", Description: "Join to devices.pk → devices.metro_pk → metros (target metro)"},
			{Name: "link_pk", Type: "VARCHAR", Description: "Join to links.pk"},
			{Name: "link_code", Type: "VARCHAR", Description: "Link code"},
			{Name: "link_type", Type: "VARCHAR", Description: "DZX (direct metro) or WAN"},
			{Name: "contributor_code", Type: "VARCHAR", Description: "Contributor code"},
			{Name: "committed_rtt", Type: "DOUBLE", Description: "Committed RTT (microseconds, from link SLA)"},
			{Name: "committed_jitter", Type: "DOUBLE", Description: "Committed jitter (microseconds, from link SLA)"},
		},
	},
	{
		Name:        "dz_device_link_latency_samples",
		Description: "RTT samples for device-to-device circuits (probes, not user traffic). When rtt_us=0, packet loss occurred. Join circuit_code → dz_device_link_circuits → dz_devices → dz_metros. Metro pair format: origin || ' → ' || target",
		Columns: []ColumnInfo{
			{Name: "circuit_code", Type: "VARCHAR", Description: "Join to dz_device_link_circuits.code → dz_device_link_circuits.origin_device_pk/target_device_pk → dz_devices → dz_metros. Metro pair: origin_metro.code || ' → ' || target_metro.code"},
			{Name: "epoch", Type: "BIGINT", Description: "Solana epoch"},
			{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
			{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
			{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds (BIGINT). rtt_us=0 = packet loss. Filter with WHERE rtt_us > 0. For arithmetic, use CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT)"},
		},
	},
	{
		Name:        "dz_internet_metro_latency_samples",
		Description: "RTT samples for metro-to-metro over public internet (probes, not user traffic). circuit_code format: 'origin → target' (e.g., 'nyc → lon'). Match bidirectionally with dz_device_link_latency_samples metro pairs",
		Columns: []ColumnInfo{
			{Name: "circuit_code", Type: "VARCHAR", Description: "Metro pair code: 'origin → target' (e.g., 'nyc → lon'). Match bidirectionally with dz_device_link_latency_samples metro pairs"},
			{Name: "data_provider", Type: "VARCHAR", Description: "Data provider (e.g., cloudping, pingdom)"},
			{Name: "epoch", Type: "BIGINT", Description: "Solana epoch"},
			{Name: "sample_index", Type: "INTEGER", Description: "Sample index within epoch (0-based)"},
			{Name: "timestamp_us", Type: "BIGINT", Description: "Timestamp (microseconds since UNIX epoch)"},
			{Name: "rtt_us", Type: "BIGINT", Description: "RTT in microseconds (BIGINT). All values valid (no packet loss). For arithmetic, use CAST(rtt_us AS BIGINT) * CAST(rtt_us AS BIGINT)"},
		},
	},
}
