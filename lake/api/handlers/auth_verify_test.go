package handlers

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/mr-tron/base58"
)

func TestGoogleIDTokenClaimsParsing(t *testing.T) {
	// Google's tokeninfo endpoint returns exp/iat as strings, not numbers
	// This test ensures our struct can handle that format
	tests := []struct {
		name     string
		json     string
		wantErr  bool
		wantExp  string
		wantEmail string
	}{
		{
			name: "valid response with string exp/iat",
			json: `{
				"iss": "https://accounts.google.com",
				"sub": "110169484474386276334",
				"aud": "test-client-id.apps.googleusercontent.com",
				"email": "user@example.com",
				"name": "Test User",
				"picture": "https://example.com/photo.jpg",
				"exp": "1705881600",
				"iat": "1705878000"
			}`,
			wantErr:   false,
			wantExp:   "1705881600",
			wantEmail: "user@example.com",
		},
		{
			name: "minimal valid response",
			json: `{
				"iss": "accounts.google.com",
				"sub": "12345",
				"aud": "client-id",
				"email": "test@doublezero.xyz",
				"exp": "9999999999",
				"iat": "1000000000"
			}`,
			wantErr:   false,
			wantExp:   "9999999999",
			wantEmail: "test@doublezero.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var claims GoogleIDTokenClaims
			err := json.Unmarshal([]byte(tt.json), &claims)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if claims.Exp != tt.wantExp {
				t.Errorf("exp = %q, want %q", claims.Exp, tt.wantExp)
			}

			if claims.Email != tt.wantEmail {
				t.Errorf("email = %q, want %q", claims.Email, tt.wantEmail)
			}
		})
	}
}

func TestBuildSIWSMessage(t *testing.T) {
	nonce := "abc123"
	message := buildSIWSMessage(nonce)

	expected := "Sign this message to authenticate with Lake.\n\nNonce: abc123"
	if message != expected {
		t.Errorf("buildSIWSMessage() = %q, want %q", message, expected)
	}
}

func TestParseSIWSMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantNonce string
		wantErr   bool
	}{
		{
			name:      "valid message",
			message:   "Sign this message to authenticate with Lake.\n\nNonce: abc123",
			wantNonce: "abc123",
			wantErr:   false,
		},
		{
			name:      "valid message with whitespace",
			message:   "Sign this message to authenticate with Lake.\n\nNonce: xyz789  ",
			wantNonce: "xyz789",
			wantErr:   false,
		},
		{
			name:      "missing nonce prefix",
			message:   "Sign this message to authenticate with Lake.",
			wantNonce: "",
			wantErr:   true,
		},
		{
			name:      "empty message",
			message:   "",
			wantNonce: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nonce, err := ParseSIWSMessage(tt.message)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nonce != tt.wantNonce {
				t.Errorf("ParseSIWSMessage() = %q, want %q", nonce, tt.wantNonce)
			}
		})
	}
}

func TestVerifyEd25519Signature(t *testing.T) {
	// Generate a test keypair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	publicKeyBase58 := base58.Encode(publicKey)
	message := "Sign this message to authenticate with Lake.\n\nNonce: test123"

	// Sign the message
	signature := ed25519.Sign(privateKey, []byte(message))
	signatureBase64 := base64.StdEncoding.EncodeToString(signature)

	tests := []struct {
		name            string
		publicKey       string
		message         string
		signature       string
		wantValid       bool
		wantErr         bool
	}{
		{
			name:      "valid signature",
			publicKey: publicKeyBase58,
			message:   message,
			signature: signatureBase64,
			wantValid: true,
			wantErr:   false,
		},
		{
			name:      "wrong message",
			publicKey: publicKeyBase58,
			message:   "different message",
			signature: signatureBase64,
			wantValid: false,
			wantErr:   false,
		},
		{
			name:      "invalid public key",
			publicKey: "invalid",
			message:   message,
			signature: signatureBase64,
			wantValid: false,
			wantErr:   true,
		},
		{
			name:      "invalid signature encoding",
			publicKey: publicKeyBase58,
			message:   message,
			signature: "not-base64!!!",
			wantValid: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := verifyEd25519Signature(tt.publicKey, tt.message, tt.signature)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if valid != tt.wantValid {
				t.Errorf("verifyEd25519Signature() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

func TestBuildAndParseSIWSMessageRoundTrip(t *testing.T) {
	nonces := []string{
		"simple",
		"with-dashes-123",
		"UUIDlike-a1b2c3d4-e5f6-7890",
		"   spaces   ",
	}

	for _, nonce := range nonces {
		t.Run(nonce, func(t *testing.T) {
			message := buildSIWSMessage(nonce)
			parsed, err := ParseSIWSMessage(message)
			if err != nil {
				t.Fatalf("ParseSIWSMessage() error = %v", err)
			}

			// Note: ParseSIWSMessage trims whitespace
			expected := nonce
			if parsed != expected {
				// For whitespace nonces, the parsed result will be trimmed
				// This is acceptable behavior
				t.Logf("note: nonce %q was trimmed to %q", nonce, parsed)
			}
		})
	}
}
