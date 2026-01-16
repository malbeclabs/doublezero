package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Plan creates a query plan based on the interpretation and data mapping.
func (p *Pipeline) Plan(ctx context.Context, interpretation *Interpretation, mapping *DataMapping, schema string, state *IterationState) (*QueryPlan, error) {
	systemPrompt := p.prompts.Plan

	// Build user prompt with context
	var userPrompt strings.Builder
	userPrompt.WriteString("## Question Interpretation\n\n")
	userPrompt.WriteString(fmt.Sprintf("- **Reframed Question**: %s\n", interpretation.Reframed))
	userPrompt.WriteString(fmt.Sprintf("- **Question Type**: %s\n", interpretation.QuestionType))
	userPrompt.WriteString(fmt.Sprintf("- **Success Criteria**: %s\n", interpretation.SuccessCriteria))

	userPrompt.WriteString("\n## Data Mapping\n\n")
	userPrompt.WriteString(fmt.Sprintf("- **Unit of Analysis**: %s\n", mapping.UnitOfAnalysis))
	userPrompt.WriteString("- **Tables**:\n")
	for _, t := range mapping.Tables {
		userPrompt.WriteString(fmt.Sprintf("  - `%s`: %s (columns: %s)\n", t.Table, t.Role, strings.Join(t.KeyColumns, ", ")))
	}
	if len(mapping.Joins) > 0 {
		userPrompt.WriteString("- **Joins**:\n")
		for _, j := range mapping.Joins {
			userPrompt.WriteString(fmt.Sprintf("  - %s %s %s ON %s\n", j.LeftTable, j.JoinType, j.RightTable, j.Condition))
		}
	}
	if len(mapping.Caveats) > 0 {
		userPrompt.WriteString(fmt.Sprintf("- **Caveats**: %s\n", strings.Join(mapping.Caveats, "; ")))
	}

	// Include schema
	userPrompt.WriteString("\n## Database Schema\n\n```\n")
	userPrompt.WriteString(schema)
	userPrompt.WriteString("\n```\n")

	// Include iteration context if this is a retry
	if state.Iteration > 1 && len(state.History) > 0 {
		userPrompt.WriteString("\n## Previous Iteration Results\n\n")
		lastHistory := state.History[len(state.History)-1]
		userPrompt.WriteString(fmt.Sprintf("Iteration %d failed inspection:\n", lastHistory.Iteration))
		for _, issue := range lastHistory.Inspection.Issues {
			userPrompt.WriteString(fmt.Sprintf("- [%s] %s\n", issue.Severity, issue.Description))
		}
		if len(lastHistory.Inspection.Suggestions) > 0 {
			userPrompt.WriteString("\nSuggestions for this iteration:\n")
			for _, s := range lastHistory.Inspection.Suggestions {
				userPrompt.WriteString(fmt.Sprintf("- %s\n", s))
			}
		}
	}

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse JSON response
	plan, err := parseQueryPlan(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query plan: %w", err)
	}

	return plan, nil
}

// parseQueryPlan parses the LLM response into a QueryPlan struct.
func parseQueryPlan(response string) (*QueryPlan, error) {
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

	var result QueryPlan
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(response))
	}

	return &result, nil
}
