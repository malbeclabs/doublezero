package v3

import (
	"fmt"
	"strings"
	"time"

	commonprompts "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/prompts"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/v3/prompts"
)

// Prompts contains all the v3 workflow prompts loaded from embedded files.
type Prompts struct {
	System        string // Main system prompt with workflow guidance and domain knowledge
	SQLContext    string // Shared SQL/domain context
	CypherContext string // Graph/Cypher domain context (optional)
	Slack         string // Slack-specific formatting guidelines
}

// GetPrompt returns the prompt content for the given name.
// This implements the workflow.PromptsProvider interface.
func (p *Prompts) GetPrompt(name string) string {
	switch name {
	case "system":
		return p.System
	default:
		return ""
	}
}

// LoadPrompts loads all v3 prompts from the embedded filesystem.
func LoadPrompts() (*Prompts, error) {
	p := &Prompts{}

	var err error

	// Load SQL_CONTEXT first (shared content)
	if p.SQLContext, err = loadCommonPrompt("SQL_CONTEXT.md"); err != nil {
		return nil, fmt.Errorf("failed to load SQL_CONTEXT: %w", err)
	}

	// Load CYPHER_CONTEXT (optional - don't fail if not present)
	p.CypherContext, _ = loadCommonPrompt("CYPHER_CONTEXT.md")

	// Load system prompt and compose with SQL_CONTEXT
	rawSystem, err := loadPrompt("SYSTEM.md")
	if err != nil {
		return nil, fmt.Errorf("failed to load SYSTEM: %w", err)
	}
	p.System = strings.ReplaceAll(rawSystem, "{{SQL_CONTEXT}}", p.SQLContext)

	// Load common prompts (Slack formatting)
	if p.Slack, err = loadCommonPrompt("SLACK.md"); err != nil {
		return nil, fmt.Errorf("failed to load SLACK: %w", err)
	}

	return p, nil
}

func loadPrompt(path string) (string, error) {
	data, err := prompts.FS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func loadCommonPrompt(path string) (string, error) {
	data, err := commonprompts.PromptsFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read common prompt %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// BuildSystemPrompt constructs the full system prompt with schema and optional format context.
func BuildSystemPrompt(basePrompt, schema, formatContext string) string {
	return BuildSystemPromptWithGraph(basePrompt, schema, "", "", formatContext)
}

// BuildSystemPromptWithGraph constructs the full system prompt with SQL schema, graph schema, and optional format context.
func BuildSystemPromptWithGraph(basePrompt, sqlSchema, graphSchema, cypherContext, formatContext string) string {
	// Add current date context at the beginning so the model knows what "today" is
	currentDate := time.Now().UTC().Format("2006-01-02")
	prompt := fmt.Sprintf("Today's date: %s (UTC)\n\n%s", currentDate, basePrompt)

	// Add SQL schema
	prompt += fmt.Sprintf("\n\n# SQL Database Schema (ClickHouse)\n\n%s", sqlSchema)

	// Add graph schema if provided
	if graphSchema != "" {
		prompt += fmt.Sprintf("\n\n%s", graphSchema) // graphSchema already includes the header
	}

	// Add Cypher context if provided
	if cypherContext != "" {
		prompt += fmt.Sprintf("\n\n%s", cypherContext)
	}

	// Add platform-specific formatting if provided
	if formatContext != "" {
		prompt += fmt.Sprintf("\n\n# Output Formatting\n\n%s", formatContext)
	}

	return prompt
}
