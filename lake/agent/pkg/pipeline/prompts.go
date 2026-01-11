package pipeline

import (
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/prompts"
)

// Prompts contains all the pipeline prompts loaded from embedded files.
type Prompts struct {
	CatalogSummary string // High-level catalog overview for decomposition
	Classify       string // Prompt for question classification (pre-step)
	Decompose      string // Prompt for breaking down questions
	Generate       string // Prompt for SQL generation
	Respond        string // Prompt for conversational responses (no data query)
	Slack          string // Slack-specific formatting guidelines (optional)
	Synthesize     string // Prompt for answer synthesis
}

// LoadPrompts loads all prompts from the embedded filesystem.
func LoadPrompts() (*Prompts, error) {
	p := &Prompts{}

	var err error
	if p.CatalogSummary, err = loadPrompt("CATALOG_SUMMARY.md"); err != nil {
		return nil, fmt.Errorf("failed to load CATALOG_SUMMARY: %w", err)
	}
	if p.Classify, err = loadPrompt("CLASSIFY.md"); err != nil {
		return nil, fmt.Errorf("failed to load CLASSIFY: %w", err)
	}
	if p.Decompose, err = loadPrompt("DECOMPOSE.md"); err != nil {
		return nil, fmt.Errorf("failed to load DECOMPOSE: %w", err)
	}
	if p.Generate, err = loadPrompt("GENERATE.md"); err != nil {
		return nil, fmt.Errorf("failed to load GENERATE: %w", err)
	}
	if p.Respond, err = loadPrompt("RESPOND.md"); err != nil {
		return nil, fmt.Errorf("failed to load RESPOND: %w", err)
	}
	if p.Slack, err = loadPrompt("SLACK.md"); err != nil {
		return nil, fmt.Errorf("failed to load SLACK: %w", err)
	}
	if p.Synthesize, err = loadPrompt("SYNTHESIZE.md"); err != nil {
		return nil, fmt.Errorf("failed to load SYNTHESIZE: %w", err)
	}

	// Inject catalog summary into decompose prompt
	p.Decompose = strings.Replace(p.Decompose, "{{CATALOG_SUMMARY}}", p.CatalogSummary, 1)

	return p, nil
}

func loadPrompt(path string) (string, error) {
	data, err := prompts.PromptsFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
