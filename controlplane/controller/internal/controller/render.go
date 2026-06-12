package controller

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"unicode"
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

type renderedField struct {
	name  string
	value string
}

// isInvalidConfigChar reports whether r cannot appear in a config token. Control
// characters break line structure; whitespace splits a value into extra tokens.
func isInvalidConfigChar(r rune) bool {
	return unicode.IsControl(r) || unicode.IsSpace(r)
}

// validateTemplateData rejects template inputs whose value would not survive
// rendering into a single config token intact.
func validateTemplateData(data templateData) error {
	d := data.Device
	if d == nil {
		return nil // template execution reports the nil device on its own
	}
	fields := []renderedField{
		{"device mgmt_vrf", d.MgmtVrf},
		{"device exchange code", d.ExchangeCode},
		{"device isis net", d.IsisNet},
		{"device vpnv4 loopback interface name", d.Vpn4vLoopbackIntfName},
		{"device ipv4 loopback interface name", d.Ipv4LoopbackIntfName},
	}
	for _, iface := range d.Interfaces {
		fields = append(fields, renderedField{"interface name", iface.Name})
		for _, topology := range iface.LinkTopologies {
			fields = append(fields, renderedField{"interface link topology", topology})
		}
		for _, segment := range iface.FlexAlgoNodeSegments {
			fields = append(fields, renderedField{"flex-algo topology name", segment.TopologyName})
		}
	}
	for _, peer := range data.Ipv4BgpPeers {
		fields = append(fields, renderedField{"ipv4 bgp peer name", peer.PeerName})
	}
	for _, peer := range data.Vpnv4BgpPeers {
		fields = append(fields, renderedField{"vpnv4 bgp peer name", peer.PeerName})
	}
	for _, topology := range data.AllTopologies {
		fields = append(fields, renderedField{"topology name", topology.Name})
	}
	for _, field := range fields {
		if strings.ContainsFunc(field.value, isInvalidConfigChar) {
			return fmt.Errorf("%s %q contains control or whitespace characters", field.name, field.value)
		}
	}
	return nil
}

func renderConfig(data templateData) (string, error) {
	if err := validateTemplateData(data); err != nil {
		return "", fmt.Errorf("refusing to render config: %w", err)
	}
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
