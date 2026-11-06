package allowlist

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExceptions(t *testing.T) {
	raw := `
inclusions:
  - pubkey: ` + pk(1) + `
    code: dz-fr5-sw01
    address: 216.155.20.62
    evidence: GetConfig capture on aws-mn-controller2, 2026-10-29
exclusions:
  - pubkey: ` + pk(2) + `
    code: fra-test-tve1
    reason: test device
`
	exc, err := ParseExceptions([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, Exceptions{
		Inclusions: []Inclusion{{Pubkey: pk(1), Code: "dz-fr5-sw01", Address: netip.MustParseAddr("216.155.20.62"), Evidence: "GetConfig capture on aws-mn-controller2, 2026-10-29"}},
		Exclusions: []Exclusion{{Pubkey: pk(2), Code: "fra-test-tve1", Reason: "test device"}},
	}, exc)
}

func TestParseExceptions_EmptyFileHasNone(t *testing.T) {
	for _, raw := range []string{"", "# nothing yet\n", "inclusions: []\nexclusions: []\n"} {
		exc, err := ParseExceptions([]byte(raw))
		require.NoError(t, err, raw)
		assert.Empty(t, exc.Inclusions)
		assert.Empty(t, exc.Exclusions)
	}
}

func TestParseExceptions_Rejects(t *testing.T) {
	inc := func(pubkey, code, address, evidence string) string {
		return "inclusions:\n  - pubkey: " + pubkey + "\n    code: " + code + "\n    address: " + address + "\n    evidence: " + evidence + "\n"
	}
	exc := func(pubkey, code, reason string) string {
		return "exclusions:\n  - pubkey: " + pubkey + "\n    code: " + code + "\n    reason: " + reason + "\n"
	}
	cases := map[string]struct{ raw, err string }{
		"unknown field": {"inclusion:\n  - pubkey: x\n", "parsing exception file: yaml: unmarshal errors:\n  line 1: field inclusion not found in type allowlist.Exceptions"},
		"bad pubkey":    {inc("nope", "dz1", "8.8.8.8", "x"), `exception file: inclusion 0: pubkey "nope" is not a base58 32-byte key`},
		"empty code":    {inc(pk(1), `""`, "8.8.8.8", "x"), "exception file: inclusion 0: device " + pk(1) + ": code is empty"},
		"private address": {inc(pk(1), "dz1", "10.1.1.1", "x"),
			`exception file: inclusion 0 (dz1): address "10.1.1.1" is not a global IPv4 address`},
		"ipv6 address": {inc(pk(1), "dz1", `"2001:4860::1"`, "x"),
			`exception file: inclusion 0 (dz1): address "2001:4860::1" is not a global IPv4 address`},
		"missing address": {"inclusions:\n  - pubkey: " + pk(1) + "\n    code: dz1\n    evidence: x\n",
			`exception file: inclusion 0 (dz1): address "invalid IP" is not a global IPv4 address`},
		"no evidence": {inc(pk(1), "dz1", "8.8.8.8", `""`),
			"exception file: inclusion 0 (dz1): evidence is empty; name the capture or log that showed the address"},
		"two inclusions for one device": {inc(pk(1), "dz1", "8.8.8.8", "x") + "  - pubkey: " + pk(1) + "\n    code: dz1\n    address: 8.8.4.4\n    evidence: x\n",
			"exception file: inclusion 1 (dz1): device " + pk(1) + " has more than one inclusion; the file holds one per device"},
		"no reason": {exc(pk(1), "dz1", `""`), "exception file: exclusion 0 (dz1): reason is empty"},
		"excluded twice": {exc(pk(1), "dz1", "x") + "  - pubkey: " + pk(1) + "\n    code: dz1\n    reason: y\n",
			"exception file: exclusion 1 (dz1): device " + pk(1) + " is excluded twice"},
		"included and excluded": {inc(pk(1), "dz1", "8.8.8.8", "x") + exc(pk(1), "dz1", "y"),
			"exception file: exclusion 0 (dz1): device " + pk(1) + " is both included and excluded"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseExceptions([]byte(tc.raw))
			assert.EqualError(t, err, tc.err)
		})
	}
}

func TestLoadExceptions_MissingFileFails(t *testing.T) {
	_, err := LoadExceptions(filepath.Join(t.TempDir(), "absent.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}
