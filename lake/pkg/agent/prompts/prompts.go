package prompts

import (
	"fmt"
	"strings"
)

// Prompts contains all the agent prompts loaded from embedded files.
type Prompts struct {
	Slack        string
	Catalog      string
	Finalization string
	Role         string
}

// BuildSystemPrompt builds the system prompt by combining role and catalog.
func (p *Prompts) BuildSystemPrompt() string {
	return p.Role + "\n\n## Catalog Reference\n\n" + p.Catalog
}

// BuildSlackSystemPrompt builds the system prompt for Slack by combining role, catalog, and Slack-specific guidelines.
func (p *Prompts) BuildSlackSystemPrompt() string {
	return p.BuildSystemPrompt() + "\n\n" + p.Slack
}

// Load loads all prompts from the embedded filesystem.
func Load() (*Prompts, error) {
	p := &Prompts{}

	var err error
	if p.Slack, err = loadPrompt("SLACK.md"); err != nil {
		return nil, fmt.Errorf("failed to load SLACK: %w", err)
	}
	if p.Catalog, err = loadPrompt("CATALOG.md"); err != nil {
		return nil, fmt.Errorf("failed to load CATALOG: %w", err)
	}
	if p.Finalization, err = loadPrompt("FINALIZATION.md"); err != nil {
		return nil, fmt.Errorf("failed to load FINALIZATION: %w", err)
	}
	if p.Role, err = loadPrompt("ROLE.md"); err != nil {
		return nil, fmt.Errorf("failed to load ROLE: %w", err)
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
