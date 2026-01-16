package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Execute runs a SQL query and captures the results.
// This is Step 3 of the pipeline.
// The questionNum is a 1-indexed question identifier for logging (e.g., Q1, Q2).
func (p *Pipeline) Execute(ctx context.Context, query GeneratedQuery, questionNum int) (ExecutedQuery, error) {
	result, err := p.cfg.Querier.Query(ctx, query.SQL)
	if err != nil {
		// Query execution infrastructure error (not a SQL error)
		return ExecutedQuery{
			GeneratedQuery: query,
			Result: QueryResult{
				SQL:   query.SQL,
				Error: fmt.Sprintf("execution error: %v", err),
			},
		}, nil
	}

	// Log the query execution
	if p.log != nil {
		if result.Error != "" {
			p.log.Info("pipeline: query returned error",
				"q", questionNum,
				"question", query.DataQuestion.Question,
				"error", result.Error)
		} else {
			p.log.Info("pipeline: query executed",
				"q", questionNum,
				"question", query.DataQuestion.Question,
				"rows", result.Count)
		}
	}

	return ExecutedQuery{
		GeneratedQuery: query,
		Result:         result,
	}, nil
}

// formatValueForLLM formats a single value for display to the LLM.
// Floats are rounded to 2 decimal places to avoid long decimals (like 3.3333333333333335)
// that can confuse the LLM into thinking they're encoded/hex values.
func formatValueForLLM(v any) string {
	switch val := v.(type) {
	case float64:
		// Round to 2 decimal places for cleaner output
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
		s := fmt.Sprintf("%v", v)
		// Truncate long values
		if len(s) > 100 {
			s = s[:97] + "..."
		}
		return s
	}
}

// FormatQueryResult formats a query result for display in the synthesis prompt.
// Uses JSON format which LLMs understand well and won't misinterpret.
func FormatQueryResult(result QueryResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	if result.Count == 0 {
		return "Query returned no results."
	}

	// Limit display to 50 rows
	maxRows := 50
	displayRows := result.Count
	if displayRows > maxRows {
		displayRows = maxRows
	}

	// Build JSON array of objects with only non-null values
	var rows []map[string]any
	for i := 0; i < displayRows && i < len(result.Rows); i++ {
		row := make(map[string]any)
		for _, col := range result.Columns {
			val := result.Rows[i][col]
			if val == nil {
				continue
			}
			// Format floats to 2 decimal places
			switch v := val.(type) {
			case float64:
				if v == float64(int64(v)) {
					row[col] = int64(v) // Whole number
				} else {
					// Round to 2 decimal places
					row[col] = float64(int64(v*100+0.5)) / 100
				}
			case float32:
				if v == float32(int32(v)) {
					row[col] = int32(v)
				} else {
					row[col] = float64(int64(float64(v)*100+0.5)) / 100
				}
			default:
				// Skip empty strings
				if s, ok := val.(string); ok && s == "" {
					continue
				}
				row[col] = val
			}
		}
		rows = append(rows, row)
	}

	// Marshal to JSON with indentation for readability
	jsonBytes, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting results: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results (%d rows):\n", result.Count))
	sb.Write(jsonBytes)

	if result.Count > maxRows {
		sb.WriteString(fmt.Sprintf("\n\n... and %d more rows", result.Count-maxRows))
	}

	return sb.String()
}
