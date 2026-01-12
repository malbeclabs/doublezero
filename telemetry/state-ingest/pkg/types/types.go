package types

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"
)

const AuthPrefixV1 = "DOUBLEZERO_V1"

const UploadURLPath = "/v1/snapshots/upload-url"
const StateToCollectPath = "/v1/state-to-collect"
const HealthzPath = "/healthz"
const ReadyzPath = "/readyz"

type UploadURLRequest struct {
	DevicePubkey      string `json:"device_pubkey"`
	SnapshotTimestamp string `json:"snapshot_timestamp"`
	SnapshotSHA256    string `json:"snapshot_sha256"`
	Kind              string `json:"kind"`
}

type UploadURLResponse struct {
	Status string `json:"status"`
	S3Key  string `json:"s3_key"`
	Upload struct {
		Method  string              `json:"method"`
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
	} `json:"upload"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

type ShowCommand struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
}

type StateToCollectResponse struct {
	ShowCommands []ShowCommand `json:"show_commands"`
	Custom       []string      `json:"custom"`
}

func CanonicalAuthMessage(prefix, method, path, timestamp string, body []byte) string {
	h := sha256.Sum256(body)
	return fmt.Sprintf("%s\nmethod:%s\npath:%s\ntimestamp:%s\nbody-sha256:%x\n",
		prefix, method, path, timestamp, h[:])
}

func RFC3339UTC(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func CanonicalRequestPath(r *http.Request) string {
	p := r.URL.EscapedPath()
	if p == "" {
		p = "/"
	}
	return p
}
