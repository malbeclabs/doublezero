package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Inspect analyzes query results to determine if they're suitable for answering the question.
func (p *Pipeline) Inspect(ctx context.Context, interpretation *Interpretation, executedQueries []pipeline.ExecutedQuery, state *IterationState) (*InspectionResult, error) {
	systemPrompt := p.prompts.Inspect

	// Build user prompt with context and results
	var userPrompt strings.Builder
	userPrompt.WriteString("## Question Context\n\n")
	userPrompt.WriteString(fmt.Sprintf("- **Original Question**: %s\n", interpretation.Reframed))
	userPrompt.WriteString(fmt.Sprintf("- **Success Criteria**: %s\n", interpretation.SuccessCriteria))
	if interpretation.FailureCriteria != "" {
		userPrompt.WriteString(fmt.Sprintf("- **Failure Criteria**: %s\n", interpretation.FailureCriteria))
	}
	userPrompt.WriteString(fmt.Sprintf("- **Current Iteration**: %d of %d\n", state.Iteration, state.MaxIterations))

	userPrompt.WriteString("\n## Query Results\n\n")
	for i, eq := range executedQueries {
		userPrompt.WriteString(fmt.Sprintf("### Query %d: %s\n\n", i+1, eq.GeneratedQuery.DataQuestion.Question))
		userPrompt.WriteString("```sql\n")
		userPrompt.WriteString(eq.Result.SQL)
		userPrompt.WriteString("\n```\n\n")

		if eq.Result.Error != "" {
			userPrompt.WriteString(fmt.Sprintf("**Error**: %s\n\n", eq.Result.Error))
		} else {
			userPrompt.WriteString(fmt.Sprintf("**Result**: %d rows\n\n", eq.Result.Count))
			if len(eq.Result.Rows) > 0 {
				// Show first few rows as sample
				userPrompt.WriteString("Sample data:\n```json\n")
				maxRows := min(5, len(eq.Result.Rows))
				sampleRows := eq.Result.Rows[:maxRows]
				jsonData, _ := json.MarshalIndent(sampleRows, "", "  ")
				userPrompt.WriteString(string(jsonData))
				userPrompt.WriteString("\n```\n\n")
			}
		}
	}

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse JSON response
	inspection, err := parseInspectionResult(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse inspection result: %w", err)
	}

	return inspection, nil
}

// parseInspectionResult parses the LLM response into an InspectionResult struct.
func parseInspectionResult(response string) (*InspectionResult, error) {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			endIdx := len(lines) - 1
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") && i > 0 {
					endIdx = i
					break
				}
			}
			startIdx := 1
			response = strings.Join(lines[startIdx:endIdx], "\n")
		}
	}

	var result InspectionResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(response))
	}

	return &result, nil
}
