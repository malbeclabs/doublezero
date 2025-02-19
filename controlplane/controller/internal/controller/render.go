package controller

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*
var f embed.FS

func renderConfig(device *Device) (string, error) {
	t, err := template.New("tunnel.tmpl").ParseFS(f, "templates/tunnel.tmpl")
	if err != nil {
		return "", fmt.Errorf("error loading tunnel template: %v", err)
	}
	var output bytes.Buffer
	if err = t.Execute(&output, device); err != nil {
		return "", fmt.Errorf("error executing template: %v", err)
	}
	return output.String(), nil
}
