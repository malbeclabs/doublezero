package ingest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58/base58"
)

type Metadata struct {
	SnapshotTimestamp string           `json:"snapshot_timestamp"`
	Command           string           `json:"command"`
	DevicePubkey      solana.PublicKey `json:"device_pubkey"`
	Kind              string           `json:"kind"`
}

type PushRequest struct {
	Metadata Metadata    `json:"metadata"`
	Data     interface{} `json:"data"`
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	pubkey     solana.PublicKey
	privKey    ed25519.PrivateKey
}

func NewClient(rawBaseURL string, pubkey solana.PublicKey, privKey ed25519.PrivateKey) (*Client, error) {
	u, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:    u,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		pubkey:     pubkey,
		privKey:    privKey,
	}, nil
}

func (c *Client) Push(ctx context.Context, kind string, req PushRequest) error {
	if req.Metadata.DevicePubkey.IsZero() {
		req.Metadata.DevicePubkey = c.pubkey
	}
	if req.Metadata.SnapshotTimestamp == "" {
		req.Metadata.SnapshotTimestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if req.Metadata.Kind == "" {
		req.Metadata.Kind = kind
	}

	path := "/v1/push"
	if kind != "" {
		path = "/v1/push/" + kind
	}

	fullURL := *c.baseURL
	fullURL.Path = path

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)

	h := sha256.Sum256(bodyBytes)
	bodyHashHex := hex.EncodeToString(h[:])

	canonical := fmt.Sprintf(
		"DOUBLEZERO_STATE_PUSH_V1\nmethod:%s\npath:%s\ntimestamp:%s\nbody-sha256:%s\n",
		"POST",
		path,
		ts,
		bodyHashHex,
	)

	sig := ed25519.Sign(c.privKey, []byte(canonical))
	sigB58 := base58.Encode(sig)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-DoubleZero-Pubkey", c.pubkey.String())
	httpReq.Header.Set("X-DoubleZero-Signature", sigB58)
	httpReq.Header.Set("X-DoubleZero-Timestamp", ts)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s: %s", resp.Status, string(b))
	}

	return nil
}
