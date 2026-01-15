package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateResponse is the expected JSON response from the generate step.
type GenerateResponse struct {
	SQL         string `json:"sql"`
	Explanation string `json:"explanation"`
}

// Generate creates a SQL query for a data question.
// This is Step 2 of the pipeline.
func (p *Pipeline) Generate(ctx context.Context, dataQuestion DataQuestion) (GeneratedQuery, error) {
	// Fetch dynamic schema from the database
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with dynamic schema
	systemPrompt := buildGeneratePrompt(p.cfg.Prompts.Generate, schema)

	userPrompt := fmt.Sprintf("Data question: %s\n\nRationale: %s", dataQuestion.Question, dataQuestion.Rationale)

	// Use cache control for GENERATE calls - the system prompt (GENERATE.md + schema)
	// is large (~13K tokens) and identical across parallel SQL generation calls
	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt, WithCacheControl())
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the response
	sql, explanation, err := parseGenerateResponse(response)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to parse generate response: %w", err)
	}

	if sql == "" {
		return GeneratedQuery{}, fmt.Errorf("no SQL generated")
	}

	return GeneratedQuery{
		DataQuestion: dataQuestion,
		SQL:          sql,
		Explanation:  explanation,
	}, nil
}

// parseGenerateResponse extracts SQL and explanation from the LLM response.
func parseGenerateResponse(response string) (sql, explanation string, err error) {
	response = strings.TrimSpace(response)

	// First, try to parse as JSON
	jsonStr := extractJSON(response)
	if jsonStr != "" {
		var parsed GenerateResponse
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil && parsed.SQL != "" {
			return cleanSQL(parsed.SQL), parsed.Explanation, nil
		}
	}

	// Fall back to extracting SQL from code blocks
	sql = extractSQLFromCodeBlocks(response)
	if sql != "" {
		// Try to extract explanation from surrounding text
		explanation = extractExplanation(response)
		return sql, explanation, nil
	}

	// Last resort: treat the whole response as SQL if it looks like SQL
	if looksLikeSQL(response) {
		return cleanSQL(response), "", nil
	}

	return "", "", fmt.Errorf("could not extract SQL from response")
}

// extractSQLFromCodeBlocks finds SQL in markdown code blocks.
func extractSQLFromCodeBlocks(response string) string {
	// Look for SQL in code blocks
	if start := strings.Index(response, "```sql"); start != -1 {
		start += 6 // len("```sql")
		if end := strings.Index(response[start:], "```"); end != -1 {
			return cleanSQL(response[start : start+end])
		}
	}

	// Look for SQL in generic code blocks
	if start := strings.Index(response, "```"); start != -1 {
		start += 3
		if end := strings.Index(response[start:], "```"); end != -1 {
			content := strings.TrimSpace(response[start : start+end])
			if looksLikeSQL(content) {
				return cleanSQL(content)
			}
		}
	}

	return ""
}

// looksLikeSQL checks if text appears to be a SQL query.
func looksLikeSQL(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	sqlKeywords := []string{"SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP"}
	for _, kw := range sqlKeywords {
		if strings.HasPrefix(upper, kw) {
			return true
		}
	}
	return false
}

// cleanSQL normalizes SQL by trimming whitespace and removing trailing semicolons.
func cleanSQL(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")
	return sql
}

// extractExplanation tries to find explanation text outside of code blocks.
func extractExplanation(response string) string {
	// Remove code blocks and get remaining text
	result := response

	for {
		start := strings.Index(result, "```")
		if start == -1 {
			break
		}
		end := strings.Index(result[start+3:], "```")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+3+end+3:]
	}

	result = strings.TrimSpace(result)
	if len(result) > 500 {
		result = result[:500] + "..."
	}

	return result
}

// buildGeneratePrompt combines the static prompt with dynamic schema.
func buildGeneratePrompt(staticPrompt, schema string) string {
	return staticPrompt + "\n\n## Database Schema\n\n```\n" + schema + "```"
}

// ZeroRowAnalysis represents the LLM's analysis of a zero-row result.
type ZeroRowAnalysis struct {
	IsSuspicious bool   `json:"is_suspicious"`
	Reasoning    string `json:"reasoning"`
	Suggestion   string `json:"suggestion"`
}

// AnalyzeZeroResult asks the LLM whether zero rows is expected or suspicious.
func (p *Pipeline) AnalyzeZeroResult(ctx context.Context, dataQuestion DataQuestion, sql string) (*ZeroRowAnalysis, error) {
	systemPrompt := `You are analyzing a ClickHouse SQL query that returned zero rows. Determine if this is expected or suspicious.

Consider:
- The question being asked - does it expect data to exist?
- The SQL query - are there filters that might be too restrictive or use wrong values?
- Common mistakes: wrong column values (e.g., 'active' vs 'activated'), wrong date ranges, incorrect joins

IMPORTANT ClickHouse behavior:
- String columns are NON-NULLABLE by default (not Nullable(String))
- In LEFT JOINs with no match, non-nullable String columns return '' (empty string), NOT NULL
- For anti-join patterns (find rows in A not in B), check for empty string: WHERE b.column = ''
- Only Nullable(String) columns return NULL on no match

Respond with JSON:
{
  "is_suspicious": true/false,
  "reasoning": "Brief explanation of why zero rows might be expected or suspicious",
  "suggestion": "If suspicious, what might be wrong with the query"
}`

	userPrompt := fmt.Sprintf(`Question: %s
Rationale: %s

SQL Query:
%s

The query returned 0 rows. Is this expected or suspicious?`, dataQuestion.Question, dataQuestion.Rationale, sql)

	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	// Parse JSON response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		// Default to not suspicious if we can't parse
		return &ZeroRowAnalysis{IsSuspicious: false, Reasoning: "Could not analyze"}, nil
	}

	var analysis ZeroRowAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return &ZeroRowAnalysis{IsSuspicious: false, Reasoning: "Could not parse analysis"}, nil
	}

	return &analysis, nil
}

// RegenerateWithZeroRows creates a new SQL query when the previous one returned zero rows.
func (p *Pipeline) RegenerateWithZeroRows(ctx context.Context, dataQuestion DataQuestion, failedSQL string, analysis *ZeroRowAnalysis) (GeneratedQuery, error) {
	// Fetch dynamic schema from the database
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with dynamic schema
	systemPrompt := buildGeneratePrompt(p.cfg.Prompts.Generate, schema)

	// Build user prompt with zero-row context
	userPrompt := fmt.Sprintf(`Data question: %s

Rationale: %s

The previous SQL query returned zero rows, which may indicate incorrect filters or values.

Previous SQL:
%s

Analysis: %s

Suggestion: %s

Please generate a corrected SQL query. Pay close attention to:
- Column values (check the sample values in the schema)
- Date/time ranges
- Join conditions
- Filter conditions`, dataQuestion.Question, dataQuestion.Rationale, failedSQL, analysis.Reasoning, analysis.Suggestion)

	// Use cache control - same system prompt as Generate
	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt, WithCacheControl())
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the response
	sql, explanation, err := parseGenerateResponse(response)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to parse generate response: %w", err)
	}

	if sql == "" {
		return GeneratedQuery{}, fmt.Errorf("no SQL generated")
	}

	return GeneratedQuery{
		DataQuestion: dataQuestion,
		SQL:          sql,
		Explanation:  explanation,
	}, nil
}

// RegenerateWithError creates a fixed SQL query based on a previous error.
func (p *Pipeline) RegenerateWithError(ctx context.Context, dataQuestion DataQuestion, failedSQL string, errorMsg string) (GeneratedQuery, error) {
	// Fetch dynamic schema from the database
	schema, err := p.cfg.SchemaFetcher.FetchSchema(ctx)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Build system prompt with dynamic schema
	systemPrompt := buildGeneratePrompt(p.cfg.Prompts.Generate, schema)

	// Build user prompt with error context
	userPrompt := fmt.Sprintf(`Data question: %s

Rationale: %s

The previous SQL query failed with an error. Please fix it.

Failed SQL:
%s

Error message:
%s

Generate a corrected SQL query that avoids this error.`, dataQuestion.Question, dataQuestion.Rationale, failedSQL, errorMsg)

	// Use cache control - same system prompt as Generate
	response, err := p.cfg.LLM.Complete(ctx, systemPrompt, userPrompt, WithCacheControl())
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse the response
	sql, explanation, err := parseGenerateResponse(response)
	if err != nil {
		return GeneratedQuery{}, fmt.Errorf("failed to parse generate response: %w", err)
	}

	if sql == "" {
		return GeneratedQuery{}, fmt.Errorf("no SQL generated")
	}

	return GeneratedQuery{
		DataQuestion: dataQuestion,
		SQL:          sql,
		Explanation:  explanation,
	}, nil
}
