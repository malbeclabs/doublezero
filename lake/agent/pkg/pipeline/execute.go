package pipeline

import (
	"context"
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
func FormatQueryResult(result QueryResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	if result.Count == 0 {
		return "Query returned no results."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Columns: %s\n", strings.Join(result.Columns, ", ")))
	sb.WriteString(fmt.Sprintf("Rows (%d total):\n", result.Count))

	// Limit display to 50 rows
	maxRows := 50
	displayRows := result.Count
	if displayRows > maxRows {
		displayRows = maxRows
	}

	for i := 0; i < displayRows && i < len(result.Rows); i++ {
		values := make([]string, len(result.Columns))
		for j, col := range result.Columns {
			values[j] = formatValueForLLM(result.Rows[i][col])
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if result.Count > maxRows {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", result.Count-maxRows))
	}

	return sb.String()
}
