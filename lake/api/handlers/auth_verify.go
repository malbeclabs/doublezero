package handlers

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mr-tron/base58"
)

// GoogleIDTokenClaims represents claims from a Google ID token
// Note: Google's tokeninfo endpoint returns exp/iat as strings, not numbers
type GoogleIDTokenClaims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Issuer  string `json:"iss"`
	Aud     string `json:"aud"`
	Exp     string `json:"exp"`
	Iat     string `json:"iat"`
}

// verifyGoogleIDToken verifies a Google ID token and returns claims
func verifyGoogleIDToken(ctx context.Context, idToken string) (*GoogleIDTokenClaims, error) {
	// Use Google's tokeninfo endpoint for verification
	// This is simpler than verifying JWTs ourselves and handles key rotation
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	if clientID == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID not configured")
	}

	url := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?id_token=%s", idToken)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token verification failed with status %d", resp.StatusCode)
	}

	var claims GoogleIDTokenClaims
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}

	// Verify audience matches our client ID
	if claims.Aud != clientID {
		return nil, fmt.Errorf("invalid audience")
	}

	// Verify issuer
	if claims.Issuer != "https://accounts.google.com" && claims.Issuer != "accounts.google.com" {
		return nil, fmt.Errorf("invalid issuer")
	}

	// Verify token is not expired
	exp, err := strconv.ParseInt(claims.Exp, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid exp claim: %w", err)
	}
	if exp < time.Now().Unix() {
		return nil, fmt.Errorf("token expired")
	}

	// Verify email is present
	if claims.Email == "" {
		return nil, fmt.Errorf("email not present in token")
	}

	return &claims, nil
}

// verifyEd25519Signature verifies an Ed25519 signature (used for Solana wallet auth)
func verifyEd25519Signature(publicKeyBase58, message, signatureBase64 string) (bool, error) {
	// Decode the public key from base58
	publicKeyBytes, err := base58.Decode(publicKeyBase58)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %w", err)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid public key size: expected %d, got %d", ed25519.PublicKeySize, len(publicKeyBytes))
	}

	// Decode the signature from base64
	signatureBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		// Try URL-safe base64
		signatureBytes, err = base64.URLEncoding.DecodeString(signatureBase64)
		if err != nil {
			// Try raw base64 (without padding)
			signatureBytes, err = base64.RawStdEncoding.DecodeString(signatureBase64)
			if err != nil {
				return false, fmt.Errorf("failed to decode signature: %w", err)
			}
		}
	}

	if len(signatureBytes) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature size: expected %d, got %d", ed25519.SignatureSize, len(signatureBytes))
	}

	// Convert message to bytes
	messageBytes := []byte(message)

	// Verify the signature
	publicKey := ed25519.PublicKey(publicKeyBytes)
	valid := ed25519.Verify(publicKey, messageBytes, signatureBytes)

	return valid, nil
}

// buildSIWSMessage builds the message format for Sign-In With Solana
func buildSIWSMessage(nonce string) string {
	// Simple message format that wallets can sign
	return fmt.Sprintf("Sign this message to authenticate with Lake.\n\nNonce: %s", nonce)
}

// ParseSIWSMessage extracts nonce from a SIWS message
func ParseSIWSMessage(message string) (string, error) {
	if !strings.Contains(message, "Nonce: ") {
		return "", fmt.Errorf("invalid message format")
	}

	parts := strings.Split(message, "Nonce: ")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid message format")
	}

	return strings.TrimSpace(parts[1]), nil
}
