package isis

import (
	_ "embed"
	"regexp"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed locations.yaml
var locationsYAML []byte

// locationConfig represents the YAML structure for location patterns.
type locationConfig struct {
	Patterns []locationPattern `yaml:"patterns"`
}

type locationPattern struct {
	Pattern     string `yaml:"pattern"`
	Location    string `yaml:"location"`
	Description string `yaml:"description"`
}

// compiledPattern holds a pre-compiled regex and its associated location info.
type compiledPattern struct {
	regex       *regexp.Regexp
	location    string
	description string
}

// Locator infers location from hostnames using pattern matching.
type Locator struct {
	patterns []compiledPattern
}

var (
	defaultLocator     *Locator
	defaultLocatorOnce sync.Once
	defaultLocatorErr  error
)

// NewLocator creates a new Locator from the embedded YAML patterns.
func NewLocator() (*Locator, error) {
	return newLocatorFromYAML(locationsYAML)
}

// DefaultLocator returns a singleton Locator instance.
// It's safe to call concurrently.
func DefaultLocator() (*Locator, error) {
	defaultLocatorOnce.Do(func() {
		defaultLocator, defaultLocatorErr = NewLocator()
	})
	return defaultLocator, defaultLocatorErr
}

// newLocatorFromYAML parses YAML and compiles patterns.
func newLocatorFromYAML(data []byte) (*Locator, error) {
	var config locationConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	patterns := make([]compiledPattern, 0, len(config.Patterns))
	for _, p := range config.Patterns {
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, compiledPattern{
			regex:       re,
			location:    p.Location,
			description: p.Description,
		})
	}

	return &Locator{patterns: patterns}, nil
}

// Infer returns the location for a hostname based on pattern matching.
// Returns "Other" if no pattern matches.
func (l *Locator) Infer(hostname string) string {
	for _, p := range l.patterns {
		if p.regex.MatchString(hostname) {
			return p.location
		}
	}
	return "Other"
}

// Description returns the human-readable description for a location.
// Returns the location itself if no description is found.
func (l *Locator) Description(location string) string {
	for _, p := range l.patterns {
		if p.location == location {
			return p.description
		}
	}
	return location
}

// AllLocations returns all known locations with their descriptions.
func (l *Locator) AllLocations() map[string]string {
	result := make(map[string]string)
	for _, p := range l.patterns {
		if _, exists := result[p.location]; !exists {
			result[p.location] = p.description
		}
	}
	return result
}
