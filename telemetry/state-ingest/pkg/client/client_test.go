package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
	"github.com/stretchr/testify/require"
)

type testSigner struct {
	mu  sync.Mutex
	got [][]byte
	err error
	sig []byte
}

func (s *testSigner) Sign(ctx context.Context, data []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, append([]byte(nil), data...))
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.sig...), nil
}

func (s *testSigner) lastMsg() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) == 0 {
		return ""
	}
	return string(s.got[len(s.got)-1])
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestTelemetry_StateIngest_Client_NewClient_OptionsApplied(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	hc := &http.Client{Timeout: 123 * time.Millisecond}
	c, err := NewClient("http://a", device, signer, WithEndpoint("http://b"), WithHTTPClient(hc))
	require.NoError(t, err)

	require.Equal(t, "http://b", c.BaseURL)
	require.Same(t, hc, c.HTTPClient)
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_SignerCalledAndRequestWellFormed(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{7}, 64)}

	var gotReqBody []byte
	var gotHeaders http.Header
	var gotPath string
	var gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotHeaders = r.Header.Clone()
		gotReqBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"s3_key":"k1",
			"upload":{"method":"PUT","url":"https://example.invalid/put","headers":{"X-Test":["ok"]}}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	snapshotTS := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	snapshot := []byte(`{"hello":"world"}`)

	resp, err := c.RequestUploadURL(context.Background(), "snmp-mib-ifmib-ifindex", snapshotTS, snapshot)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "ok", resp.Status)
	require.Equal(t, "k1", resp.S3Key)

	require.Equal(t, types.UploadURLPath, gotPath)
	require.Equal(t, http.MethodPost, gotMethod)

	var decoded types.UploadURLRequest
	require.NoError(t, json.Unmarshal(gotReqBody, &decoded))
	require.Equal(t, device.String(), decoded.DevicePubkey)
	require.Equal(t, snapshotTS.UTC().Format(time.RFC3339), decoded.SnapshotTimestamp)
	require.Equal(t, "snmp-mib-ifmib-ifindex", decoded.Kind)
	wantSnapHash := sha256.Sum256(snapshot)
	require.Equal(t, hex.EncodeToString(wantSnapHash[:]), decoded.SnapshotSHA256)

	require.Equal(t, "application/json", gotHeaders.Get("Content-Type"))
	require.Equal(t, device.String(), gotHeaders.Get("X-DZ-Device"))
	require.NotEmpty(t, gotHeaders.Get("X-DZ-Timestamp"))
	require.NotEmpty(t, gotHeaders.Get("X-DZ-Signature"))

	ts := gotHeaders.Get("X-DZ-Timestamp")

	wantCanonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, types.UploadURLPath, ts, gotReqBody)

	require.Equal(t, wantCanonical, signer.lastMsg())
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_HTTPNon200_JSONErrorResponse(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope","code":401}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)

	var he *HTTPError
	require.ErrorAs(t, err, &he)
	require.Equal(t, 401, he.StatusCode)
	require.Contains(t, he.Message, "nope")
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_HTTPNon200_NonJSONBodyFallback(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized) // text/plain; charset=utf-8
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)

	var he *HTTPError
	require.ErrorAs(t, err, &he)
	require.Equal(t, 401, he.StatusCode)
	require.Contains(t, he.Message, "nope")
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_NonOKStatusField(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"no","s3_key":"k","upload":{"method":"PUT","url":"u","headers":{}}}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-ok status")
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_SignerErrorPropagates(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{err: errors.New("boom")}

	c, err := NewClient("http://example.invalid", device, signer, WithHTTPClient(&http.Client{
		Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("http should not be called if signer fails")
			return nil, nil
		}),
	}))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_PutsSnapshotWithHeadersAndReturnsKey(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{9}, 64)}

	var gotPutHeaders http.Header
	var gotPutBody []byte
	var gotPutMethod string

	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPutMethod = r.Method
		gotPutHeaders = r.Header.Clone()
		gotPutBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(putSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := types.UploadURLResponse{Status: "ok", S3Key: "the-key"}
		out.Upload.Method = http.MethodPut
		out.Upload.URL = putSrv.URL
		out.Upload.Headers = map[string][]string{}
		_, _ = w.Write(mustJSON(t, out))
	}))
	t.Cleanup(apiSrv.Close)

	c, err := NewClient(apiSrv.URL, device, signer, WithHTTPClient(apiSrv.Client()))
	require.NoError(t, err)

	snapshot := []byte(`{"snap":true}`)
	key, err := c.UploadSnapshot(context.Background(), "snmp-mib-ifmib-ifindex", time.Now(), snapshot)
	require.NoError(t, err)
	require.Equal(t, "the-key", key)

	require.Equal(t, http.MethodPut, gotPutMethod)
	require.Equal(t, snapshot, gotPutBody)
	require.Equal(t, "application/json", gotPutHeaders.Get("Content-Type"), "client should set content-type if absent from signed headers")
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_FiltersUnsafePresignedHeaders(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{7}, 64)}

	var gotPutHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case types.UploadURLPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"ok",
				"s3_key":"k1",
				"upload":{
					"method":"PUT",
					"url":"` + srvURLForTest(r) + `/put",
					"headers":{
						"Host":["example.invalid"],
						"Content-Type":["application/json"],
						"X-Test":["ok"],
						"Content-Length":["999"]
					}
				}
			}`))
		case "/put":
			gotPutHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	snapshotTS := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	snapshot := []byte(`{"hello":"world"}`)

	key, err := c.UploadSnapshot(context.Background(), "snmp-mib-ifmib-ifindex", snapshotTS, snapshot)
	require.NoError(t, err)
	require.Equal(t, "k1", key)

	require.NotNil(t, gotPutHeaders)

	require.Equal(t, "application/json", gotPutHeaders.Get("Content-Type"))
	require.Equal(t, "ok", gotPutHeaders.Get("X-Test"))

	require.NotEqual(t, "example.invalid", gotPutHeaders.Get("Host"))

	require.NotEqual(t, "999", gotPutHeaders.Get("Content-Length"))
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_PutNonOKReturnsBody(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("denied"))
	}))
	t.Cleanup(putSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := types.UploadURLResponse{Status: "ok", S3Key: "k"}
		out.Upload.Method = http.MethodPut
		out.Upload.URL = putSrv.URL
		out.Upload.Headers = map[string][]string{}
		_, _ = w.Write(mustJSON(t, out))
	}))
	t.Cleanup(apiSrv.Close)

	c, err := NewClient(apiSrv.URL, device, signer, WithHTTPClient(apiSrv.Client()))
	require.NoError(t, err)

	_, err = c.UploadSnapshot(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "S3 upload failed")
	require.Contains(t, err.Error(), "status=403")
	require.Contains(t, err.Error(), "denied")
}

func TestTelemetry_StateIngest_Client_WithNilHTTPClientFallsBackToDefault(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"s3_key":"k1",
			"upload":{"method":"PUT","url":"https://example.invalid/put","headers":{}}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer)
	require.NoError(t, err)
	c.HTTPClient = nil

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.NoError(t, err)
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_PathIsConstant(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"s3_key":"k1",
			"upload":{"method":"PUT","url":"https://example.invalid/put","headers":{}}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(strings.TrimRight(srv.URL, "/"), device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, types.UploadURLPath, got)
}

func TestTelemetry_StateIngest_Client_BaseURL_TrailingSlashNormalized(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"s3_key":"k",
			"upload":{"method":"PUT","url":"https://example.invalid","headers":{}}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL+"/", device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, types.UploadURLPath, gotPath)
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_InvalidJSONResponseReturnsError(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok",`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)
}

func TestTelemetry_StateIngest_Client_RequestUploadURL_HTTPTransportErrorPropagates(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	want := errors.New("dial boom")
	hc := &http.Client{
		Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return nil, want
		}),
	}

	c, err := NewClient("http://example.invalid", device, signer, WithHTTPClient(hc))
	require.NoError(t, err)

	_, err = c.RequestUploadURL(context.Background(), "k", time.Now(), []byte(`{}`))
	require.ErrorIs(t, err, want)
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_RequestUploadURLFailureShortCircuitsPUT(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{2}, 64)}

	putCalled := false

	hc := &http.Client{
		Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPut {
				putCalled = true
				t.Fatal("PUT should not be called when RequestUploadURL fails")
			}
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"error":"nope","code":401}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		}),
	}

	c, err := NewClient("http://example.invalid", device, signer, WithHTTPClient(hc))
	require.NoError(t, err)

	_, err = c.UploadSnapshot(context.Background(), "k", time.Now(), []byte(`{}`))
	require.Error(t, err)

	var he *HTTPError
	require.ErrorAs(t, err, &he)
	require.Equal(t, 401, he.StatusCode)

	require.False(t, putCalled)
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_PutAddsAllHeaderValues(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{9}, 64)}

	var got http.Header
	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(putSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := types.UploadURLResponse{Status: "ok", S3Key: "k"}
		out.Upload.Method = http.MethodPut
		out.Upload.URL = putSrv.URL
		out.Upload.Headers = map[string][]string{
			"X-Multi": {"a", "b"},
			"X-One":   {"c"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mustJSON(t, out))
	}))
	t.Cleanup(apiSrv.Close)

	c, err := NewClient(apiSrv.URL, device, signer, WithHTTPClient(apiSrv.Client()))
	require.NoError(t, err)

	_, err = c.UploadSnapshot(context.Background(), "k", time.Now(), []byte(`{"x":1}`))
	require.NoError(t, err)

	require.Equal(t, []string{"a", "b"}, got.Values("X-Multi"))
	require.Equal(t, "c", got.Get("X-One"))
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_PutDoesNotOverwriteContentTypeIfProvided(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{9}, 64)}

	var gotCT string
	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(putSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := types.UploadURLResponse{Status: "ok", S3Key: "k"}
		out.Upload.Method = http.MethodPut
		out.Upload.URL = putSrv.URL
		out.Upload.Headers = map[string][]string{"Content-Type": {"application/x-ndjson"}}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mustJSON(t, out))
	}))
	t.Cleanup(apiSrv.Close)

	c, err := NewClient(apiSrv.URL, device, signer, WithHTTPClient(apiSrv.Client()))
	require.NoError(t, err)

	_, err = c.UploadSnapshot(context.Background(), "k", time.Now(), []byte(`{"x":1}`))
	require.NoError(t, err)

	require.Equal(t, "application/x-ndjson", gotCT)
}

func TestTelemetry_StateIngest_Client_UploadSnapshot_WithNilHTTPClientFallsBackToDefaultForPUT(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{9}, 64)}

	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(putSrv.Close)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := types.UploadURLResponse{Status: "ok", S3Key: "k"}
		out.Upload.Method = http.MethodPut
		out.Upload.URL = putSrv.URL
		out.Upload.Headers = map[string][]string{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mustJSON(t, out))
	}))
	t.Cleanup(apiSrv.Close)

	c, err := NewClient(apiSrv.URL, device, signer)
	require.NoError(t, err)
	c.HTTPClient = nil

	_, err = c.UploadSnapshot(context.Background(), "k", time.Now(), []byte(`{"x":1}`))
	require.NoError(t, err)
}

func srvURLForTest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_Success_ReturnsShowCommands(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	expectedShowCommands := []types.ShowCommand{
		{Kind: "snmp-mib-ifmib-ifindex", Command: "show snmp mib ifmib ifindex"},
		{Kind: "isis-database-detail", Command: "show isis database detail"},
	}

	var gotPath string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		resp := types.StateToCollectResponse{ShowCommands: expectedShowCommands}
		_, _ = w.Write(mustJSON(t, resp))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	resp, err := c.GetStateToCollect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, expectedShowCommands, resp.ShowCommands)

	require.Equal(t, types.StateToCollectPath, gotPath)
	require.Equal(t, http.MethodGet, gotMethod)
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_HTTPNon200_JSONErrorResponse(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error","code":500}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.GetStateToCollect(context.Background())
	require.Error(t, err)

	var he *HTTPError
	require.ErrorAs(t, err, &he)
	require.Equal(t, 500, he.StatusCode)
	require.Contains(t, he.Message, "server error")
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_HTTPNon200_NonJSONBodyFallback(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.GetStateToCollect(context.Background())
	require.Error(t, err)

	var he *HTTPError
	require.ErrorAs(t, err, &he)
	require.Equal(t, 404, he.StatusCode)
	require.Contains(t, he.Message, "not found")
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_InvalidJSONResponseReturnsError(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"show_commands":{`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.GetStateToCollect(context.Background())
	require.Error(t, err)
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_HTTPTransportErrorPropagates(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	want := errors.New("dial boom")
	hc := &http.Client{
		Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return nil, want
		}),
	}

	c, err := NewClient("http://example.invalid", device, signer, WithHTTPClient(hc))
	require.NoError(t, err)

	_, err = c.GetStateToCollect(context.Background())
	require.ErrorIs(t, err, want)
}

func TestTelemetry_StateIngest_Client_GetStateToCollect_PathIsConstant(t *testing.T) {
	t.Parallel()

	device := solana.NewWallet().PublicKey()
	signer := &testSigner{sig: bytes.Repeat([]byte{1}, 64)}

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := types.StateToCollectResponse{ShowCommands: []types.ShowCommand{}}
		_, _ = w.Write(mustJSON(t, resp))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(strings.TrimRight(srv.URL, "/"), device, signer, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	_, err = c.GetStateToCollect(context.Background())
	require.NoError(t, err)
	require.Equal(t, types.StateToCollectPath, got)
}
