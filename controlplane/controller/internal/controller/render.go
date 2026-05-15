package controller

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"text/template"
)

//go:embed templates/*
var f embed.FS

// templateFuncs registers helpers available inside tunnel.tmpl. fail aborts
// rendering with a clear error when a template branch hits a state that
// should be unreachable in normal flow (e.g. a physical interface with zero
// MTU after the role-based MTU assignment in toInterface).
var templateFuncs = template.FuncMap{
	"fail": func(msg string) (string, error) { return "", errors.New(msg) },
}

func renderConfig(data templateData) (string, error) {
	t, err := template.New("tunnel.tmpl").Funcs(templateFuncs).ParseFS(f, "templates/tunnel.tmpl")
	if err != nil {
		return "", fmt.Errorf("error loading tunnel template: %v", err)
	}
	var output bytes.Buffer
	if err = t.Execute(&output, data); err != nil {
		return "", fmt.Errorf("error executing template: %v", err)
	}
	return output.String(), nil
}
