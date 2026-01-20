package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
)

// HTTPQuerier implements Querier using HTTP calls to ClickHouse.
type HTTPQuerier struct {
	clickhouseURL string
}

// NewHTTPQuerier creates a new HTTP-based querier.
func NewHTTPQuerier(clickhouseURL string) *HTTPQuerier {
	return &HTTPQuerier{
		clickhouseURL: clickhouseURL,
	}
}

// Query executes a SQL query and returns the result.
func (q *HTTPQuerier) Query(ctx context.Context, sql string) (QueryResult, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), ";")
	query := sql + " FORMAT JSON"

	req, err := http.NewRequestWithContext(ctx, "POST", q.clickhouseURL, strings.NewReader(query))
	if err != nil {
		return QueryResult{SQL: sql, Error: "Failed to create request: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return QueryResult{SQL: sql, Error: "Failed to connect to database: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return QueryResult{SQL: sql, Error: "Failed to read response: " + err.Error()}, nil
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := strings.TrimSpace(string(body))
		if len(errMsg) > 500 {
			errMsg = errMsg[:500] + "..."
		}
		return QueryResult{SQL: sql, Error: errMsg}, nil
	}

	var chResp struct {
		Meta []struct {
			Name string `json:"name"`
		} `json:"meta"`
		Data []map[string]any `json:"data"`
	}

	if err := json.Unmarshal(body, &chResp); err != nil {
		return QueryResult{SQL: sql, Error: "Failed to parse response: " + err.Error()}, nil
	}

	columns := make([]string, 0, len(chResp.Meta))
	for _, m := range chResp.Meta {
		columns = append(columns, m.Name)
	}

	// Sanitize rows to replace NaN/Inf values with nil (JSON-safe)
	SanitizeRows(chResp.Data)

	result := QueryResult{
		SQL:     sql,
		Columns: columns,
		Rows:    chResp.Data,
		Count:   len(chResp.Data),
	}

	// Generate formatted output for the LLM
	result.Formatted = formatResult(result)

	return result, nil
}

// FormatValue formats a single value for display to the LLM.
// Pointer types are dereferenced to get the actual value - this is critical
// for ClickHouse Decimal types which are scanned as pointers.
// This is exported so it can be used by other packages (api/handlers, slack, etc).
func FormatValue(v any) string {
	if v == nil {
		return ""
	}

	// Handle pointer types by dereferencing them
	// This is critical for ClickHouse Decimal types which scan as pointers
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		return FormatValue(rv.Elem().Interface())
	}

	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%v", val)
	case float32:
		return fmt.Sprintf("%v", val)
	case string:
		return val
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatResult creates a human-readable format of the query result.
func formatResult(result QueryResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	if len(result.Rows) == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results (%d rows):\n", len(result.Rows)))
	sb.WriteString("Columns: " + strings.Join(result.Columns, " | ") + "\n")
	sb.WriteString(strings.Repeat("-", 40) + "\n")

	// Limit output to first 50 rows
	maxRows := min(50, len(result.Rows))

	for i := range maxRows {
		row := result.Rows[i]
		var values []string
		for _, col := range result.Columns {
			values = append(values, FormatValue(row[col]))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}

// SanitizeRows replaces NaN and Inf float values with nil to ensure JSON serialization works.
// ClickHouse can return NaN for operations like division by zero, but JSON doesn't support NaN.
func SanitizeRows(rows []map[string]any) {
	for _, row := range rows {
		for key, val := range row {
			if f, ok := val.(float64); ok {
				if math.IsNaN(f) || math.IsInf(f, 0) {
					row[key] = nil
				}
			}
		}
	}
}
