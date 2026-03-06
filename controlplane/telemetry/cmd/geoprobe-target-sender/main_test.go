package main

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
)

func TestParsePubkey_Valid(t *testing.T) {
	wallet := solana.NewWallet()
	pk58 := wallet.PublicKey().String()

	got, err := parsePubkey(pk58)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := wallet.PublicKey()
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestParsePubkey_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"not base58", "not-a-valid-pubkey!!!"},
		{"too short", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePubkey(tt.input)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestFormatRTT(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"sub-millisecond", 500 * time.Microsecond, "0.500ms"},
		{"one millisecond", 1 * time.Millisecond, "1.000ms"},
		{"typical rtt", 12534 * time.Microsecond, "12.534ms"},
		{"zero", 0, "0.000ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRTT(tt.duration)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFormatNsAsMs(t *testing.T) {
	tests := []struct {
		name     string
		ns       uint64
		expected string
	}{
		{"zero", 0, "0.000ms"},
		{"one millisecond", 1_000_000, "1.000ms"},
		{"sub-millisecond", 500_000, "0.500ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNsAsMs(tt.ns)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestAbbreviatePubkey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"short key", "abc", "abc"},
		{"exactly 10", "1234567890", "1234567890"},
		{"long key", "FSM7abc123456zmQ", "FSM7...6zmQ"},
		{"full base58", "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM", "9WzD...AWWM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := abbreviatePubkey(tt.input)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestProbeOutput_JSON(t *testing.T) {
	output := probeOutput{
		Timestamp:           "2025-01-15T14:23:45Z",
		Seq:                 1,
		ProbeMeasuredRttMs:  12.534,
		TargetMeasuredRttMs: 5.200,
		Reply0ProbeSigValid: true,
		Reply0SigValid:      true,
		Reply1ProbeSigValid: true,
		Reply1SigValid:      true,
		AuthorityPubkey:     "FSM7abc123456zmQ",
		GeoprobePubkey:      "ABCD1234xyz",
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded probeOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Seq != 1 {
		t.Errorf("expected seq=1, got %d", decoded.Seq)
	}
	if decoded.ProbeMeasuredRttMs != 12.534 {
		t.Errorf("expected probe_measured_rtt_ms=12.534, got %f", decoded.ProbeMeasuredRttMs)
	}
	if decoded.TargetMeasuredRttMs != 5.200 {
		t.Errorf("expected target_measured_rtt_ms=5.200, got %f", decoded.TargetMeasuredRttMs)
	}
	if decoded.AuthorityPubkey != "FSM7abc123456zmQ" {
		t.Errorf("expected authority_pubkey=FSM7abc123456zmQ, got %s", decoded.AuthorityPubkey)
	}
	if decoded.GeoprobePubkey != "ABCD1234xyz" {
		t.Errorf("expected geoprobe_pubkey=ABCD1234xyz, got %s", decoded.GeoprobePubkey)
	}
}

func TestProbeOutput_TimeoutJSON(t *testing.T) {
	output := probeOutput{
		Timestamp:           "2025-01-15T14:23:47Z",
		Seq:                 3,
		ProbeMeasuredRttMs:  -1,
		TargetMeasuredRttMs: -1,
		Error:               "timeout",
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded["error"] != "timeout" {
		t.Errorf("expected error=timeout, got %v", decoded["error"])
	}
	if decoded["probe_measured_rtt_ms"].(float64) != -1 {
		t.Errorf("expected probe_measured_rtt_ms=-1, got %v", decoded["probe_measured_rtt_ms"])
	}
	// omitempty fields should not be present.
	if _, ok := decoded["authority_pubkey"]; ok {
		t.Error("expected authority_pubkey to be omitted for timeout")
	}
	if _, ok := decoded["geoprobe_pubkey"]; ok {
		t.Error("expected geoprobe_pubkey to be omitted for timeout")
	}
	if _, ok := decoded["reply0_sig_valid"]; ok {
		t.Error("expected reply0_sig_valid to be omitted for timeout")
	}
}

func TestProbeOutput_SuccessJSON_OmitsError(t *testing.T) {
	output := probeOutput{
		Timestamp:           "2025-01-15T14:23:45Z",
		Seq:                 1,
		ProbeMeasuredRttMs:  5.0,
		TargetMeasuredRttMs: 3.0,
		Reply0ProbeSigValid: true,
		Reply0SigValid:      true,
		Reply1ProbeSigValid: true,
		Reply1SigValid:      true,
		AuthorityPubkey:     "test",
		GeoprobePubkey:      "test2",
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := decoded["error"]; ok {
		t.Error("expected error to be omitted for successful probe")
	}
}

func TestNewEd25519Signer_Integration(t *testing.T) {
	wallet := solana.NewWallet()
	signer := signed.NewEd25519Signer(ed25519.PrivateKey(wallet.PrivateKey))

	pub := signer.Public()
	if len(pub) != 32 {
		t.Fatalf("expected 32-byte public key, got %d bytes", len(pub))
	}

	expected := wallet.PublicKey()
	var got [32]byte
	copy(got[:], pub)
	if got != expected {
		t.Errorf("signer public key doesn't match wallet public key")
	}
}
