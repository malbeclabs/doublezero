package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// VisualizeRequest is the incoming request for visualization recommendation.
type VisualizeRequest struct {
	Columns    []string `json:"columns"`
	SampleRows [][]any  `json:"sampleRows"`
	RowCount   int      `json:"rowCount"`
	Query      string   `json:"query"`
}

// VisualizeResponse is the recommendation returned to the UI.
type VisualizeResponse struct {
	Recommended bool     `json:"recommended"`
	ChartType   string   `json:"chartType,omitempty"`
	XAxis       string   `json:"xAxis,omitempty"`
	YAxis       []string `json:"yAxis,omitempty"`
	Reasoning   string   `json:"reasoning,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func RecommendVisualization(w http.ResponseWriter, r *http.Request) {
	var req VisualizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Columns) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VisualizeResponse{Recommended: false, Reasoning: "No columns provided"})
		return
	}

	// Check if we have Anthropic API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VisualizeResponse{Recommended: false, Error: "ANTHROPIC_API_KEY not set"})
		return
	}

	// Build prompt
	prompt := buildVisualizePrompt(req)

	// Call Anthropic
	client := anthropic.NewClient()
	start := time.Now()
	message, err := client.Messages.New(r.Context(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	duration := time.Since(start)
	metrics.RecordAnthropicRequest("messages", duration, err)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VisualizeResponse{Recommended: false, Error: internalError("Failed to recommend visualization", err)})
		return
	}
	metrics.RecordAnthropicTokens(message.Usage.InputTokens, message.Usage.OutputTokens)

	// Extract response text
	var responseText string
	for _, block := range message.Content {
		if block.Type == "text" {
			responseText = block.Text
			break
		}
	}

	// Parse JSON response
	response := parseVisualizeResponse(responseText)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func buildVisualizePrompt(req VisualizeRequest) string {
	columnsStr := strings.Join(req.Columns, ", ")
	sampleDataStr := formatSampleData(req.Columns, req.SampleRows)

	return fmt.Sprintf(`You are a data visualization expert. Given SQL query results, recommend the best chart type.

Columns: %s
Sample data (first rows):
%s
Total rows: %d
SQL Query: %s

Analyze this data and recommend a visualization. Consider:
- Bar charts: categorical X axis with numeric Y values
- Line charts: temporal/sequential X axis with numeric Y values
- Pie charts: single categorical column with counts/sums, best with <=10 categories
- Area charts: temporal X axis with numeric Y values (good for cumulative data)
- Scatter charts: two numeric columns to show correlation

Respond with a JSON object (no markdown, just the JSON):
{
  "recommended": true or false,
  "chartType": "bar" | "line" | "pie" | "area" | "scatter",
  "xAxis": "column_name for X axis",
  "yAxis": ["column_name(s) for Y axis"],
  "reasoning": "brief explanation of why this visualization was chosen"
}

Set "recommended" to false if:
- Data doesn't fit any visualization well
- Only one column or only one row
- All values are null
- Data is purely textual with no meaningful numeric aggregations`, columnsStr, sampleDataStr, req.RowCount, req.Query)
}

func formatSampleData(columns []string, rows [][]any) string {
	if len(rows) == 0 {
		return "(no data)"
	}

	var sb strings.Builder
	// Limit to 10 rows
	maxRows := min(10, len(rows))

	for i := 0; i < maxRows; i++ {
		row := rows[i]
		var parts []string
		for j, col := range columns {
			if j < len(row) {
				val := row[j]
				parts = append(parts, fmt.Sprintf("%s: %s", col, formatValue(val)))
			}
		}
		sb.WriteString("  " + strings.Join(parts, ", ") + "\n")
	}

	return sb.String()
}

func formatValue(v any) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		if len(val) > 50 {
			return `"` + val[:50] + `..."`
		}
		return `"` + val + `"`
	case float64:
		return fmt.Sprintf("%v", val)
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func parseVisualizeResponse(text string) VisualizeResponse {
	// Try to extract JSON from the response
	text = strings.TrimSpace(text)

	// Remove markdown code blocks if present
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock || !strings.HasPrefix(line, "```") {
				jsonLines = append(jsonLines, line)
			}
		}
		text = strings.Join(jsonLines, "\n")
	}

	var response VisualizeResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		// Try to find JSON object in the text
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start >= 0 && end > start {
			jsonStr := text[start : end+1]
			if err := json.Unmarshal([]byte(jsonStr), &response); err != nil {
				return VisualizeResponse{Recommended: false, Error: "Failed to parse LLM response"}
			}
		} else {
			return VisualizeResponse{Recommended: false, Error: "Failed to parse LLM response"}
		}
	}

	return response
}
