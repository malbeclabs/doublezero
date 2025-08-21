package fixtures

import (
	"bytes"
	"os"
	"text/template"
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

var templateFuncs = template.FuncMap{
	"seq": seq,
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
