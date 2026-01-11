package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// Execute runs a SQL query and captures the results.
// This is Step 3 of the pipeline.
func (p *Pipeline) Execute(ctx context.Context, query GeneratedQuery) (ExecutedQuery, error) {
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
				"question", query.DataQuestion.Question,
				"error", result.Error)
		} else {
			p.log.Info("pipeline: query executed",
				"question", query.DataQuestion.Question,
				"rows", result.Count)
		}
	}

	return ExecutedQuery{
		GeneratedQuery: query,
		Result:         result,
	}, nil
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
			v := result.Rows[i][col]
			s := fmt.Sprintf("%v", v)
			// Truncate long values
			if len(s) > 100 {
				s = s[:97] + "..."
			}
			values[j] = s
		}
		sb.WriteString(strings.Join(values, " | ") + "\n")
	}

	if result.Count > maxRows {
		sb.WriteString(fmt.Sprintf("... and %d more rows\n", result.Count-maxRows))
	}

	return sb.String()
}
