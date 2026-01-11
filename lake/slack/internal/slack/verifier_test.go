package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAI_Slack_VerifySlackSignature(t *testing.T) {
	t.Parallel()

	signingSecret := "test-secret"
	body := []byte(`{"type":"event_callback","event":{"type":"message"}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Create valid signature
	sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(sigBase))
	validSignature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name          string
		timestamp     string
		signature     string
		body          []byte
		signingSecret string
		want          bool
	}{
		{
			name:          "valid signature",
			timestamp:     timestamp,
			signature:     validSignature,
			body:          body,
			signingSecret: signingSecret,
			want:          true,
		},
		{
			name:          "missing timestamp",
			timestamp:     "",
			signature:     validSignature,
			body:          body,
			signingSecret: signingSecret,
			want:          false,
		},
		{
			name:          "missing signature",
			timestamp:     timestamp,
			signature:     "",
			body:          body,
			signingSecret: signingSecret,
			want:          false,
		},
		{
			name:          "invalid signature",
			timestamp:     timestamp,
			signature:     "v0=invalid",
			body:          body,
			signingSecret: signingSecret,
			want:          false,
		},
		{
			name:          "wrong signing secret",
			timestamp:     timestamp,
			signature:     validSignature,
			body:          body,
			signingSecret: "wrong-secret",
			want:          false,
		},
		{
			name:          "old timestamp (replay attack)",
			timestamp:     strconv.FormatInt(time.Now().Unix()-400, 10), // 400 seconds ago
			signature:     validSignature,
			body:          body,
			signingSecret: signingSecret,
			want:          false,
		},
		{
			name:          "invalid timestamp format",
			timestamp:     "not-a-number",
			signature:     validSignature,
			body:          body,
			signingSecret: signingSecret,
			want:          false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("X-Slack-Request-Timestamp", tt.timestamp)
			req.Header.Set("X-Slack-Signature", tt.signature)

			got := VerifySlackSignature(req, tt.body, tt.signingSecret)
			require.Equal(t, tt.want, got)
		})
	}
}
