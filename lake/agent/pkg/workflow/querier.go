package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// formatValue formats a single value for display to the LLM.
// Floats are rounded to 2 decimal places to avoid long decimals (like 3.3333333333333335)
// that can confuse the LLM into thinking they're encoded values.
func formatValue(v any) string {
	switch val := v.(type) {
	case float64:
		// Round to 2 decimal places for cleaner output
		// This prevents the LLM from misinterpreting long decimals as encoded data
		if val == float64(int64(val)) {
			return fmt.Sprintf("%.0f", val) // Whole number, no decimals
		}
		return fmt.Sprintf("%.2f", val)
	case float32:
		if val == float32(int32(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%.2f", val)
	case nil:
		return ""
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
			values = append(values, formatValue(row[col]))
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if len(result.Rows) > 50 {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(result.Rows)-50))
	}

	return sb.String()
}
