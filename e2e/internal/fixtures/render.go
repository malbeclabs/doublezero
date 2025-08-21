package fixtures

import (
	"fmt"

	e2e "github.com/malbeclabs/doublezero/e2e"
	"github.com/malbeclabs/doublezero/pkg/fixtures"
)

// RenderTemplate is a wrapper around the shared fixtures package
func RenderTemplate(templateContent string, data any) (string, error) {
	return fixtures.RenderTemplate(templateContent, data)
}

// RenderFile is a wrapper around the shared fixtures package
func RenderFile(filepath string, data any) (string, error) {
	return fixtures.RenderFile(filepath, data)
}

// Render reads a fixture from the embedded filesystem and renders it as a template
func Render(fixturePath string, data any) (string, error) {
	fixture, err := e2e.FS.ReadFile(fixturePath)
	if err != nil {
		return "", fmt.Errorf("error reading fixture: %w", err)
	}
	return fixtures.RenderTemplate(string(fixture), data)
}
