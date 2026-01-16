package v2

import (
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v2/prompts"
)

// Prompts contains all the v2 pipeline prompts loaded from embedded files.
type Prompts struct {
	Interpret  string // Prompt for analytical reframing of questions
	Map        string // Prompt for mapping to data reality
	Plan       string // Prompt for query planning
	Inspect    string // Prompt for result inspection
	Synthesize string // Prompt for answer synthesis
}

// GetPrompt returns the prompt content for the given name.
// This implements the pipeline.PromptsProvider interface.
func (p *Prompts) GetPrompt(name string) string {
	switch name {
	case "interpret":
		return p.Interpret
	case "map":
		return p.Map
	case "plan":
		return p.Plan
	case "inspect":
		return p.Inspect
	case "synthesize":
		return p.Synthesize
	default:
		return ""
	}
}

// LoadPrompts loads all v2 prompts from the embedded filesystem.
func LoadPrompts() (*Prompts, error) {
	p := &Prompts{}

	var err error
	if p.Interpret, err = loadPrompt("INTERPRET.md"); err != nil {
		return nil, fmt.Errorf("failed to load INTERPRET: %w", err)
	}
	if p.Map, err = loadPrompt("MAP.md"); err != nil {
		return nil, fmt.Errorf("failed to load MAP: %w", err)
	}
	if p.Plan, err = loadPrompt("PLAN.md"); err != nil {
		return nil, fmt.Errorf("failed to load PLAN: %w", err)
	}
	if p.Inspect, err = loadPrompt("INSPECT.md"); err != nil {
		return nil, fmt.Errorf("failed to load INSPECT: %w", err)
	}
	if p.Synthesize, err = loadPrompt("SYNTHESIZE.md"); err != nil {
		return nil, fmt.Errorf("failed to load SYNTHESIZE: %w", err)
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
