package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// HTTPSchemaFetcher fetches schema from ClickHouse via HTTP.
type HTTPSchemaFetcher struct {
	ClickhouseURL string
	Database      string // defaults to "default" if empty
	Username      string // optional
	Password      string // optional
}

// NewHTTPSchemaFetcher creates a new HTTPSchemaFetcher.
func NewHTTPSchemaFetcher(clickhouseURL string) *HTTPSchemaFetcher {
	return &HTTPSchemaFetcher{
		ClickhouseURL: clickhouseURL,
		Database:      "default",
	}
}

// NewHTTPSchemaFetcherWithAuth creates a new HTTPSchemaFetcher with authentication.
func NewHTTPSchemaFetcherWithAuth(clickhouseURL, database, username, password string) *HTTPSchemaFetcher {
	if database == "" {
		database = "default"
	}
	return &HTTPSchemaFetcher{
		ClickhouseURL: clickhouseURL,
		Database:      database,
		Username:      username,
		Password:      password,
	}
}

// FetchSchema retrieves table columns and view definitions from ClickHouse.
func (f *HTTPSchemaFetcher) FetchSchema(ctx context.Context) (string, error) {
	// Fetch columns from system.columns
	columns, err := f.fetchColumns(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch columns: %w", err)
	}

	// Fetch view definitions from system.tables
	views, err := f.fetchViews(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch views: %w", err)
	}

	// Enrich categorical columns with sample values
	f.enrichWithSampleValues(ctx, columns)

	// Format schema as readable text
	schema := formatSchema(columns, views)
	return schema, nil
}

type columnInfo struct {
	Table        string   `json:"table"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	SampleValues []string `json:"-"` // populated separately for categorical columns
}

type viewInfo struct {
	Name     string `json:"name"`
	AsSelect string `json:"as_select"`
}

// doQuery executes a query against ClickHouse and returns the response body.
func (f *HTTPSchemaFetcher) doQuery(ctx context.Context, query string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", f.ClickhouseURL+"/?query="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}

	// Add authentication if provided
	if f.Username != "" {
		req.SetBasicAuth(f.Username, f.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clickhouse error: %s", string(body))
	}

	return body, nil
}

func (f *HTTPSchemaFetcher) fetchColumns(ctx context.Context) ([]columnInfo, error) {
	query := fmt.Sprintf(`
		SELECT
			table,
			name,
			type
		FROM system.columns
		WHERE database = '%s'
		  AND table NOT LIKE 'stg_%%'
		ORDER BY table, position
		FORMAT JSON
	`, f.Database)

	body, err := f.doQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []columnInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

func (f *HTTPSchemaFetcher) fetchViews(ctx context.Context) ([]viewInfo, error) {
	query := fmt.Sprintf(`
		SELECT
			name,
			as_select
		FROM system.tables
		WHERE database = '%s'
		  AND engine = 'View'
		  AND name NOT LIKE 'stg_%%'
		FORMAT JSON
	`, f.Database)

	body, err := f.doQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []viewInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// isCategoricalType returns true if the column type should have sample values displayed.
func isCategoricalType(colType string) bool {
	t := strings.ToLower(colType)
	// Match String, Enum, LowCardinality types that aren't IDs or timestamps
	if strings.Contains(t, "enum") {
		return true
	}
	if strings.Contains(t, "lowcardinality") && strings.Contains(t, "string") {
		return true
	}
	// Plain String columns that look like status/type fields
	if t == "string" || t == "nullable(string)" {
		return true
	}
	return false
}

// shouldSkipColumn returns true for columns that shouldn't have samples fetched.
func shouldSkipColumn(colName string) bool {
	name := strings.ToLower(colName)
	// Skip ID fields, timestamps, and other high-cardinality columns
	skipSuffixes := []string{"_id", "_key", "_code", "_at", "_time", "_timestamp", "_date", "_hash", "_pubkey", "_address"}
	for _, suffix := range skipSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	skipPrefixes := []string{"id_", "uuid_"}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	skipExact := []string{"id", "uuid", "name", "description", "comment", "message", "error", "reason"}
	for _, exact := range skipExact {
		if name == exact {
			return true
		}
	}
	return false
}

// enrichWithSampleValues fetches sample values for categorical columns.
func (f *HTTPSchemaFetcher) enrichWithSampleValues(ctx context.Context, columns []columnInfo) {
	// Group columns by table to batch queries
	tableColumns := make(map[string][]*columnInfo)
	for i := range columns {
		col := &columns[i]
		if isCategoricalType(col.Type) && !shouldSkipColumn(col.Name) {
			tableColumns[col.Table] = append(tableColumns[col.Table], col)
		}
	}

	// Fetch samples for each table (limit concurrent queries)
	for table, cols := range tableColumns {
		// Build a single query to get samples for all categorical columns in this table
		for _, col := range cols {
			samples, err := f.fetchColumnSamples(ctx, table, col.Name)
			if err == nil && len(samples) > 0 && len(samples) <= 15 {
				// Only include if there's a reasonable number of distinct values
				col.SampleValues = samples
			}
		}
	}
}

// fetchColumnSamples returns distinct values for a column.
func (f *HTTPSchemaFetcher) fetchColumnSamples(ctx context.Context, table, column string) ([]string, error) {
	// Query for distinct values, limited to 20 to detect high cardinality
	query := fmt.Sprintf(`
		SELECT DISTINCT %s
		FROM %s
		WHERE %s IS NOT NULL AND %s != ''
		LIMIT 20
		FORMAT JSON
	`, column, table, column, column)

	body, err := f.doQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	samples := make([]string, 0, len(result.Data))
	for _, row := range result.Data {
		if val, ok := row[column]; ok {
			if s, ok := val.(string); ok && s != "" {
				samples = append(samples, s)
			}
		}
	}

	return samples, nil
}

func formatSchema(columns []columnInfo, views []viewInfo) string {
	// Build view definitions map
	viewDefs := make(map[string]string)
	for _, v := range views {
		viewDefs[v.Name] = v.AsSelect
	}

	// Collect unique table names and categorize them
	tableSet := make(map[string]bool)
	for _, col := range columns {
		tableSet[col.Table] = true
	}

	var currentViews, historyTables, factTables, otherTables []string
	for table := range tableSet {
		switch {
		case strings.HasSuffix(table, "_current"):
			currentViews = append(currentViews, table)
		case strings.HasPrefix(table, "dim_") && strings.HasSuffix(table, "_history"):
			historyTables = append(historyTables, table)
		case strings.HasPrefix(table, "fact_"):
			factTables = append(factTables, table)
		default:
			otherTables = append(otherTables, table)
		}
	}

	// Sort for consistent output
	sort.Strings(currentViews)
	sort.Strings(historyTables)
	sort.Strings(factTables)
	sort.Strings(otherTables)

	var sb strings.Builder

	// Write table index at the top
	sb.WriteString("## AVAILABLE TABLES (use ONLY these exact names)\n\n")

	if len(currentViews) > 0 {
		sb.WriteString("Current state views (for current/live data):\n")
		for _, t := range currentViews {
			sb.WriteString("  - " + t + "\n")
		}
		sb.WriteString("\n")
	}

	if len(historyTables) > 0 {
		sb.WriteString("History tables (for point-in-time queries, use snapshot_ts and is_deleted columns):\n")
		for _, t := range historyTables {
			sb.WriteString("  - " + t + "\n")
		}
		sb.WriteString("\n")
	}

	if len(factTables) > 0 {
		sb.WriteString("Fact tables (time-series metrics and events):\n")
		for _, t := range factTables {
			sb.WriteString("  - " + t + "\n")
		}
		sb.WriteString("\n")
	}

	if len(otherTables) > 0 {
		sb.WriteString("Other tables:\n")
		for _, t := range otherTables {
			sb.WriteString("  - " + t + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n## TABLE DETAILS\n\n")

	// Write detailed schema
	currentTable := ""
	for _, col := range columns {
		if col.Table != currentTable {
			if currentTable != "" {
				// Add view definition if this was a view
				if def, ok := viewDefs[currentTable]; ok {
					sb.WriteString("  Definition: " + def + "\n")
				}
				sb.WriteString("\n")
			}
			currentTable = col.Table
			if _, isView := viewDefs[col.Table]; isView {
				sb.WriteString(col.Table + " (VIEW):\n")
			} else {
				sb.WriteString(col.Table + ":\n")
			}
		}

		// Format column with optional FK marker and sample values
		colLine := "  - " + col.Name + " (" + col.Type + ")"
		if strings.HasSuffix(col.Name, "_pk") {
			colLine += " [FK]"
		}
		if len(col.SampleValues) > 0 {
			colLine += " values: " + strings.Join(col.SampleValues, ", ")
		}
		sb.WriteString(colLine + "\n")
	}

	// Handle last table's view definition
	if def, ok := viewDefs[currentTable]; ok {
		sb.WriteString("  Definition: " + def + "\n")
	}

	return sb.String()
}
