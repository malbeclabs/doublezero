package fixtures

import (
	"bytes"
	"fmt"
	"text/template"

	e2e "github.com/malbeclabs/doublezero/e2e"
)

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

func Render(fixturePath string, data any) (string, error) {
	fixture, err := e2e.FS.ReadFile(fixturePath)
	if err != nil {
		return "", fmt.Errorf("error reading fixture: %w", err)
	}

	var buf bytes.Buffer
	tmpl := template.New("").Funcs(templateFuncs)
	err = template.Must(tmpl.Parse(string(fixture))).Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("error executing template: %w", err)
	}

	return buf.String(), nil
}
