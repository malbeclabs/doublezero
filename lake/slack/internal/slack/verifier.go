package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// VerifySlackSignature verifies the Slack request signature
func VerifySlackSignature(r *http.Request, body []byte, signingSecret string) bool {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")

	if timestamp == "" || signature == "" {
		return false
	}

	// Check timestamp to prevent replay attacks (within 5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix()-ts > 300 {
		return false
	}

	// Create signature base string
	sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(sigBase))
	expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Use constant-time comparison
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
