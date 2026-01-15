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
	if p.cfg.FormatContext != "" {
		systemPrompt = systemPrompt + "\n\n" + p.cfg.FormatContext
	}
	userPrompt := fmt.Sprintf(`User Question: %s

Data gathered:
%s

Please synthesize a clear, comprehensive answer to the user's question based on the data above.`, userQuestion, resultsContext.String())

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM completion failed: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// assessQueryConfidence evaluates the reliability of a query result.
// Only flags actual errors - zero results is not considered low confidence
// since the LLM can determine from context whether zero results is expected.
func assessQueryConfidence(eq ExecutedQuery) string {
	if eq.Result.Error != "" {
		return "LOW - Query failed with error"
	}
	return "HIGH"
}
