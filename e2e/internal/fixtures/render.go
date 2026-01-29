package fixtures

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	e2e "github.com/malbeclabs/doublezero/e2e"
)

// seq generates a sequence of integers from start to end (inclusive)
func seq(start, end int) []int {
	if start > end {
		return []int{}
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// add returns the sum of two integers
func add(a, b int) int {
	return a + b
}

// sub returns the difference of two integers
func sub(a, b int) int {
	return a - b
}

var templateFuncs = template.FuncMap{
	"seq": seq,
	"add": add,
	"sub": sub,
}

// RenderTemplate renders a template string with the given data
func RenderTemplate(templateContent string, data any) (string, error) {
	var buf bytes.Buffer
	tmpl := template.New("").Funcs(templateFuncs)
	tmpl, err := tmpl.Parse(templateContent)
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderFile reads a file and renders it as a template with the given data
func RenderFile(filepath string, data any) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}
	return RenderTemplate(string(content), data)
}

// Render reads a fixture from the embedded filesystem and renders it as a template
func Render(fixturePath string, data any) (string, error) {
	fixture, err := e2e.FS.ReadFile(fixturePath)
	if err != nil {
		return "", fmt.Errorf("error reading fixture: %w", err)
	}
	return RenderTemplate(string(fixture), data)
}
