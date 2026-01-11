package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// Synthesize creates a final answer based on the query results.
// This is Step 4 of the pipeline.
func (p *Pipeline) Synthesize(ctx context.Context, userQuestion string, executed []ExecutedQuery) (string, error) {
	// Build the context for synthesis
	var resultsContext strings.Builder

	for i, eq := range executed {
		resultsContext.WriteString(fmt.Sprintf("## Data Question %d\n", i+1))
		resultsContext.WriteString(fmt.Sprintf("**Question**: %s\n", eq.GeneratedQuery.DataQuestion.Question))
		resultsContext.WriteString(fmt.Sprintf("**Rationale**: %s\n", eq.GeneratedQuery.DataQuestion.Rationale))
		resultsContext.WriteString(fmt.Sprintf("**SQL**: ```sql\n%s\n```\n", eq.GeneratedQuery.SQL))

		// Add confidence indicator
		confidence := assessQueryConfidence(eq)
		resultsContext.WriteString(fmt.Sprintf("**Confidence**: %s\n", confidence))

		resultsContext.WriteString(fmt.Sprintf("**Results**:\n%s\n\n", FormatQueryResult(eq.Result)))
	}

	systemPrompt := p.cfg.Prompts.Synthesize
	userPrompt := fmt.Sprintf(`User Question: %s

Data gathered:
%s

Please synthesize a clear, comprehensive answer to the user's question based on the data above.`, userQuestion, resultsContext.String())

	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM completion failed: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// assessQueryConfidence evaluates the reliability of a query result.
func assessQueryConfidence(eq ExecutedQuery) string {
	// Error case
	if eq.Result.Error != "" {
		return "LOW - Query failed with error"
	}

	// Zero rows - potentially suspicious
	if eq.Result.Count == 0 {
		// Check if the question implies expectation of data
		question := strings.ToLower(eq.GeneratedQuery.DataQuestion.Question)
		// Questions starting with "how many" or "what is the count" might legitimately be zero
		if strings.Contains(question, "how many") || strings.Contains(question, "count") {
			return "MEDIUM - Zero results (may be expected for count queries)"
		}
		// Questions asking for specific entities probably expect results
		if strings.Contains(question, "which") || strings.Contains(question, "list") || strings.Contains(question, "what are") {
			return "LOW - Zero results (query may have incorrect filters)"
		}
		return "MEDIUM - Zero results"
	}

	// Very few rows might indicate overly restrictive filters
	if eq.Result.Count == 1 {
		return "HIGH - Single result returned"
	}

	if eq.Result.Count < 5 {
		return "HIGH - Small result set"
	}

	// Normal case
	return "HIGH - Query executed successfully"
}
