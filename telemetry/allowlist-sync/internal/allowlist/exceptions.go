package allowlist

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"

	"github.com/mr-tron/base58"
	"gopkg.in/yaml.v3"
)

// Exceptions is the reviewed exception file from infra (spec section 2,
// step 3), mounted from a ConfigMap. For example:
//
//	inclusions:
//	  - pubkey: <device pubkey>
//	    code: xyz01-sw01
//	    address: <global IPv4 address>
//	    evidence: <the capture or log that showed the address>
//	exclusions:
//	  - pubkey: <device pubkey>
//	    code: lab01-test01
//	    reason: <why the device stays off the list>
type Exceptions struct {
	// Inclusions are addresses a device calls from that the ledger does not
	// hold yet. One entry per device.
	Inclusions []Inclusion `yaml:"inclusions"`
	// Exclusions are devices whose addresses stay off the list. They are also
	// left out of the report and the per-device gauges.
	Exclusions []Exclusion `yaml:"exclusions"`
}

type Inclusion struct {
	Pubkey   string     `yaml:"pubkey"`
	Code     string     `yaml:"code"`
	Address  netip.Addr `yaml:"address"`
	Evidence string     `yaml:"evidence"`
}

type Exclusion struct {
	Pubkey string `yaml:"pubkey"`
	Code   string `yaml:"code"`
	Reason string `yaml:"reason"`
}

// LoadExceptions reads and validates the exception file. Any error fails the
// pass, so the last applied list stays.
func LoadExceptions(path string) (Exceptions, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Exceptions{}, fmt.Errorf("reading exception file: %w", err)
	}
	return ParseExceptions(raw)
}

func ParseExceptions(raw []byte) (Exceptions, error) {
	var exc Exceptions
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&exc); err != nil && !errors.Is(err, io.EOF) {
		return Exceptions{}, fmt.Errorf("parsing exception file: %w", err)
	}
	// The file is one document. Entries after a "---" line would otherwise be
	// dropped with no error, and a dropped exclusion lists that device.
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		if err != nil {
			return Exceptions{}, fmt.Errorf("parsing exception file: %w", err)
		}
		return Exceptions{}, fmt.Errorf("parsing exception file: it holds more than one YAML document (a second starts at line %d); merge them into one", next.Line)
	}
	if err := exc.validate(); err != nil {
		return Exceptions{}, fmt.Errorf("exception file: %w", err)
	}
	return exc, nil
}

func (e Exceptions) validate() error {
	included := map[string]bool{}
	for i, in := range e.Inclusions {
		if err := validatePubkeyAndCode(in.Pubkey, in.Code); err != nil {
			return fmt.Errorf("inclusion %d: %w", i, err)
		}
		if !isGlobalIPv4(in.Address) {
			return fmt.Errorf("inclusion %d (%s): address %q is not a global IPv4 address", i, in.Code, in.Address)
		}
		if strings.TrimSpace(in.Evidence) == "" {
			return fmt.Errorf("inclusion %d (%s): evidence is empty; name the capture or log that showed the address", i, in.Code)
		}
		if included[in.Pubkey] {
			return fmt.Errorf("inclusion %d (%s): device %s has more than one inclusion; the file holds one per device", i, in.Code, in.Pubkey)
		}
		included[in.Pubkey] = true
	}
	excluded := map[string]bool{}
	for i, ex := range e.Exclusions {
		if err := validatePubkeyAndCode(ex.Pubkey, ex.Code); err != nil {
			return fmt.Errorf("exclusion %d: %w", i, err)
		}
		if strings.TrimSpace(ex.Reason) == "" {
			return fmt.Errorf("exclusion %d (%s): reason is empty", i, ex.Code)
		}
		if excluded[ex.Pubkey] {
			return fmt.Errorf("exclusion %d (%s): device %s is excluded twice", i, ex.Code, ex.Pubkey)
		}
		if included[ex.Pubkey] {
			return fmt.Errorf("exclusion %d (%s): device %s is both included and excluded", i, ex.Code, ex.Pubkey)
		}
		excluded[ex.Pubkey] = true
	}
	return nil
}

func validatePubkeyAndCode(pubkey, code string) error {
	b, err := base58.Decode(pubkey)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("pubkey %q is not a base58 32-byte key", pubkey)
	}
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("device %s: code is empty", pubkey)
	}
	return nil
}
