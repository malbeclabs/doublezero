package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
	"github.com/mr-tron/base58"
)

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("request failed: status=%d", e.StatusCode)
	}
	return fmt.Sprintf("request failed: status=%d error=%s", e.StatusCode, e.Message)
}

type Option func(*Client)

func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.BaseURL = endpoint }
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.HTTPClient = httpClient }
}

type Signer interface {
	Sign(ctx context.Context, data []byte) ([]byte, error)
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	signer     Signer
	device     solana.PublicKey
}

func NewClient(baseURL string, device solana.PublicKey, signer Signer, opts ...Option) (*Client, error) {
	c := &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		signer:     signer,
		device:     device,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func decodeServerError(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") && len(b) > 0 {
		var er types.ErrorResponse
		if json.Unmarshal(b, &er) == nil && er.Error != "" {
			code := er.Code
			if code == 0 {
				code = resp.StatusCode
			}
			return &HTTPError{StatusCode: code, Message: er.Error}
		}
	}

	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = resp.Status
	}
	return &HTTPError{StatusCode: resp.StatusCode, Message: msg}
}
func (c *Client) RequestUploadURL(ctx context.Context, kind string, snapshotTS time.Time, data []byte) (*types.UploadURLResponse, error) {
	snapHash := sha256.Sum256(data)
	snapHashHex := hex.EncodeToString(snapHash[:])

	bodyStruct := types.UploadURLRequest{
		DevicePubkey:      c.device.String(),
		SnapshotTimestamp: snapshotTS.UTC().Format(time.RFC3339),
		SnapshotSHA256:    snapHashHex,
		Kind:              kind,
	}
	bodyBytes, err := json.Marshal(bodyStruct)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + types.UploadURLPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	canonicalPath := req.URL.EscapedPath()
	if canonicalPath == "" {
		canonicalPath = "/"
	}

	canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, req.Method, canonicalPath, now, bodyBytes)

	sig, err := c.signer.Sign(ctx, []byte(canonical))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DZ-Device", c.device.String())
	req.Header.Set("X-DZ-Timestamp", now)
	req.Header.Set("X-DZ-Signature", base58.Encode(sig))

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeServerError(resp)
	}

	var out types.UploadURLResponse
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
		ck := http.CanonicalHeaderKey(k)
		if ck == "Host" || ck == "Content-Length" {
			continue
		}
		for _, v := range vs {
			req.Header.Add(ck, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return "", fmt.Errorf("S3 upload failed: status=%d body=%s", resp.StatusCode, msg)
	}

	return uploadInfo.S3Key, nil
}
