package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Map maps the interpreted question to the data reality (tables, joins, caveats).
func (p *Pipeline) Map(ctx context.Context, interpretation *Interpretation, schema string) (*DataMapping, error) {
	systemPrompt := p.prompts.Map

	// Build user prompt with interpretation and schema
	var userPrompt strings.Builder
	userPrompt.WriteString("## Question Interpretation\n\n")
	userPrompt.WriteString(fmt.Sprintf("- **Question Type**: %s\n", interpretation.QuestionType))
	userPrompt.WriteString(fmt.Sprintf("- **Entities**: %s\n", strings.Join(interpretation.Entities, ", ")))
	if interpretation.TimeFrame != "" {
		userPrompt.WriteString(fmt.Sprintf("- **Time Frame**: %s\n", interpretation.TimeFrame))
	}
	userPrompt.WriteString(fmt.Sprintf("- **Success Criteria**: %s\n", interpretation.SuccessCriteria))
	if interpretation.FailureCriteria != "" {
		userPrompt.WriteString(fmt.Sprintf("- **Failure Criteria**: %s\n", interpretation.FailureCriteria))
	}
	userPrompt.WriteString(fmt.Sprintf("- **Reframed Question**: %s\n", interpretation.Reframed))
	userPrompt.WriteString("\n## Database Schema\n\n```\n")
	userPrompt.WriteString(schema)
	userPrompt.WriteString("\n```\n")

	response, err := p.trackLLMCall(ctx, systemPrompt, userPrompt.String())
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse JSON response
	mapping, err := parseDataMapping(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data mapping: %w", err)
	}

	return mapping, nil
}

// parseDataMapping parses the LLM response into a DataMapping struct.
func parseDataMapping(response string) (*DataMapping, error) {
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

	var result DataMapping
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(response))
	}

	return &result, nil
}
