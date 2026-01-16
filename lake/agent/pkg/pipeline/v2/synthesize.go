package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

// Synthesize creates the final answer from the query results.
func (p *Pipeline) Synthesize(ctx context.Context, userQuestion string, interpretation *Interpretation, executedQueries []pipeline.ExecutedQuery, inspection *InspectionResult) (string, error) {
	systemPrompt := p.prompts.Synthesize

	// Apply format context if configured
	if p.cfg.FormatContext != "" {
		systemPrompt = systemPrompt + "\n\n## Format Guidelines\n\n" + p.cfg.FormatContext
	}

	// Build user prompt with all context
	var userPrompt strings.Builder
	userPrompt.WriteString("## Original Question\n\n")
	userPrompt.WriteString(userQuestion)
	userPrompt.WriteString("\n\n")

	userPrompt.WriteString("## Question Analysis\n\n")
	userPrompt.WriteString(fmt.Sprintf("- **Type**: %s\n", interpretation.QuestionType))
	userPrompt.WriteString(fmt.Sprintf("- **Reframed**: %s\n", interpretation.Reframed))
	userPrompt.WriteString(fmt.Sprintf("- **Success Criteria**: %s\n", interpretation.SuccessCriteria))

	if inspection != nil {
		userPrompt.WriteString(fmt.Sprintf("\n## Data Quality Assessment\n\n"))
		userPrompt.WriteString(fmt.Sprintf("- **Confidence**: %.0f%%\n", inspection.Confidence*100))
		if len(inspection.Issues) > 0 {
			userPrompt.WriteString("- **Issues**:\n")
			for _, issue := range inspection.Issues {
				userPrompt.WriteString(fmt.Sprintf("  - [%s] %s\n", issue.Severity, issue.Description))
			}
		}
		if len(inspection.Learnings) > 0 {
			userPrompt.WriteString(fmt.Sprintf("- **Learnings**: %s\n", strings.Join(inspection.Learnings, "; ")))
		}
	}

	userPrompt.WriteString("\n## Query Results\n\n")
	for i, eq := range executedQueries {
		userPrompt.WriteString(fmt.Sprintf("### Q%d: %s\n\n", i+1, eq.GeneratedQuery.DataQuestion.Question))

		if eq.Result.Error != "" {
			userPrompt.WriteString(fmt.Sprintf("**Error**: %s\n\n", eq.Result.Error))
			continue
		}

		userPrompt.WriteString(fmt.Sprintf("**Result**: %d rows\n\n", eq.Result.Count))

		if len(eq.Result.Rows) > 0 {
			// Include all result data in JSON format
			userPrompt.WriteString("```json\n")
			jsonData, _ := json.MarshalIndent(eq.Result.Rows, "", "  ")
			userPrompt.WriteString(string(jsonData))
			userPrompt.WriteString("\n```\n\n")
		}
	}

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return "", fmt.Errorf("LLM completion failed: %w", err)
	}

	return strings.TrimSpace(response), nil
}
