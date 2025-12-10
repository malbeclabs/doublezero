package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58/base58"
)

const (
	authPrefix = "DOUBLEZERO_V1"
	apiPath    = "/v1/snapshots/upload-url"
)

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

type Option func(*Client)

func WithEndpoint(endpoint string) Option {
	return func(c *Client) {
		c.BaseURL = endpoint
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.HTTPClient = httpClient
	}
}

type Signer interface {
	PublicKey() solana.PublicKey
	Sign(ctx context.Context, data []byte) ([]byte, error)
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	signer     Signer
}

func NewClient(baseURL string, signer Signer, opts ...Option) (*Client, error) {
	c := &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		signer:     signer,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Client) RequestUploadURL(ctx context.Context, kind string, snapshotTS time.Time, data []byte) (*UploadURLResponse, error) {
	snapHash := sha256.Sum256(data)
	snapHashHex := hex.EncodeToString(snapHash[:])

	bodyStruct := UploadURLRequest{
		DevicePubkey:      c.signer.PublicKey().String(),
		SnapshotTimestamp: snapshotTS.UTC().Format(time.RFC3339),
		SnapshotSHA256:    snapHashHex,
		Kind:              kind,
	}
	bodyBytes, err := json.Marshal(bodyStruct)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	bodyHash := sha256.Sum256(bodyBytes)
	canonical := fmt.Sprintf(
		"%s\nmethod:%s\npath:%s\ntimestamp:%s\nbody-sha256:%x\n",
		authPrefix,
		http.MethodPost,
		apiPath,
		now,
		bodyHash[:],
	)
	sig, err := c.signer.Sign(ctx, []byte(canonical))
	if err != nil {
		return nil, err
	}
	sigB58 := base58.Encode(sig[:])

	url := c.BaseURL + apiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DZ-Pubkey", c.signer.PublicKey().String())
	req.Header.Set("X-DZ-Timestamp", now)
	req.Header.Set("X-DZ-Signature", sigB58)

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload-url request failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	var out UploadURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Status != "ok" {
		return nil, fmt.Errorf("server returned non-ok status: %s", out.Status)
	}
	return &out, nil
}

func (c *Client) UploadSnapshot(ctx context.Context, kind string, snapshotTS time.Time, snapshotJSON []byte) (string, error) {
	uploadInfo, err := c.RequestUploadURL(ctx, kind, snapshotTS, snapshotJSON)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, uploadInfo.Upload.Method, uploadInfo.Upload.URL, bytes.NewReader(snapshotJSON))
	if err != nil {
		return "", err
	}
	for k, vs := range uploadInfo.Upload.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("S3 upload failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	return uploadInfo.S3Key, nil
}
