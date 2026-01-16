package v1

import (
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1/prompts"
)

// Prompts contains all the pipeline prompts loaded from embedded files.
type Prompts struct {
	CatalogSummary string // High-level catalog overview for decomposition
	Classify       string // Prompt for question classification (pre-step)
	Decompose      string // Prompt for breaking down questions
	FollowUp       string // Prompt for generating follow-up question suggestions
	Generate       string // Prompt for SQL generation
	Respond        string // Prompt for conversational responses (no data query)
	Slack          string // Slack-specific formatting guidelines (optional)
	Synthesize     string // Prompt for answer synthesis
}

// GetPrompt returns the prompt content for the given name.
// This implements the pipeline.PromptsProvider interface.
func (p *Prompts) GetPrompt(name string) string {
	switch name {
	case "catalog_summary":
		return p.CatalogSummary
	case "classify":
		return p.Classify
	case "decompose":
		return p.Decompose
	case "followup":
		return p.FollowUp
	case "generate":
		return p.Generate
	case "respond":
		return p.Respond
	case "slack":
		return p.Slack
	case "synthesize":
		return p.Synthesize
	default:
		return ""
	}
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
	if p.FollowUp, err = loadPrompt("FOLLOWUP.md"); err != nil {
		return nil, fmt.Errorf("failed to load FOLLOWUP: %w", err)
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
	data, err := prompts.FS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
