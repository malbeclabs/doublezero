package fixtures

import (
	"bytes"
	"fmt"
	"text/template"

	e2e "github.com/malbeclabs/doublezero/e2e_new"
)

func Render(fixturePath string, data any) (string, error) {
	fixture, err := e2e.FS.ReadFile(fixturePath)
	if err != nil {
		return "", fmt.Errorf("error reading fixture: %w", err)
	}

	var buf bytes.Buffer
	err = template.Must(template.New("").Parse(string(fixture))).Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("error executing template: %w", err)
	}

	return buf.String(), nil
}
