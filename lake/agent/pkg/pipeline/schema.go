package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	var sb strings.Builder
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
		if len(col.SampleValues) > 0 {
			sb.WriteString("  - " + col.Name + " (" + col.Type + ") values: " + strings.Join(col.SampleValues, ", ") + "\n")
		} else {
			sb.WriteString("  - " + col.Name + " (" + col.Type + ")\n")
		}
	}

	// Handle last table's view definition
	if def, ok := viewDefs[currentTable]; ok {
		sb.WriteString("  Definition: " + def + "\n")
	}

	return sb.String()
}
