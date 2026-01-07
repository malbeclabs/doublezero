package prompts

import (
	"fmt"
	"strings"
)

// Prompts contains all the agent prompts loaded from embedded files.
type Prompts struct {
	Identity     string
	Constraints  string
	Workflow     string
	Catalog      string
	Formatting   string
	Examples     string
	Finalization string
}

// BuildSystemPrompt builds the complete system prompt.
// Order matters for LLM attention:
// 1. Identity (primacy - who you are)
// 2. Constraints (hard rules up front)
// 3. Workflow (how to operate)
// 4. Catalog (reference material)
// 5. Formatting (output rules)
// 6. Examples (recency - concrete patterns)
func (p *Prompts) BuildSystemPrompt() string {
	sections := []string{
		p.Identity,
		p.Constraints,
		p.Workflow,
		p.Catalog,
		p.Formatting,
		p.Examples,
	}

	var nonEmpty []string
	for _, s := range sections {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}

	return strings.Join(nonEmpty, "\n\n---\n\n")
}

// BuildSlackSystemPrompt builds the system prompt for Slack.
// Formatting rules are already Slack-optimized in the new architecture.
func (p *Prompts) BuildSlackSystemPrompt() string {
	return p.BuildSystemPrompt()
}

// Load loads all prompts from the embedded filesystem.
func Load() (*Prompts, error) {
	p := &Prompts{}

	var err error

	// Required files
	if p.Identity, err = loadPrompt("IDENTITY.md"); err != nil {
		return nil, fmt.Errorf("failed to load IDENTITY: %w", err)
	}
	if p.Constraints, err = loadPrompt("CONSTRAINTS.md"); err != nil {
		return nil, fmt.Errorf("failed to load CONSTRAINTS: %w", err)
	}
	if p.Workflow, err = loadPrompt("WORKFLOW.md"); err != nil {
		return nil, fmt.Errorf("failed to load WORKFLOW: %w", err)
	}
	if p.Catalog, err = loadPrompt("CATALOG.md"); err != nil {
		return nil, fmt.Errorf("failed to load CATALOG: %w", err)
	}
	if p.Formatting, err = loadPrompt("FORMATTING.md"); err != nil {
		return nil, fmt.Errorf("failed to load FORMATTING: %w", err)
	}
	if p.Finalization, err = loadPrompt("FINALIZATION.md"); err != nil {
		return nil, fmt.Errorf("failed to load FINALIZATION: %w", err)
	}

	// Optional files
	if examples, err := loadPrompt("EXAMPLES.md"); err == nil {
		p.Examples = examples
	}

	return p, nil
}

func loadPrompt(path string) (string, error) {
	data, err := PromptsFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
