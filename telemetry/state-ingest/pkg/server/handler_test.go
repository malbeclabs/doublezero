package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

type mockPresignClient struct {
	PresignPutObjectFunc func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error)
}

func (m mockPresignClient) PresignPutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
	return m.PresignPutObjectFunc(ctx, input, opts...)
}

func mustErrResp(t *testing.T, rr *httptest.ResponseRecorder) types.ErrorResponse {
	t.Helper()
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	var er types.ErrorResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &er))
	return er
}

func newTestDevice(t *testing.T) (device serviceability.Device, devicePK string, metricsPriv ed25519.PrivateKey) {
	t.Helper()

	var devPK [32]byte
	_, err := rand.Read(devPK[:])
	require.NoError(t, err)
	copy(device.PubKey[:], devPK[:])
	devicePK = base58.Encode(devPK[:])

	metricsPub, metricsPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	copy(device.MetricsPublisherPubKey[:], metricsPub[:])

	return device, devicePK, metricsPriv
}

func signReq(t *testing.T, authPrefix string, clk clockwork.Clock, metricsPriv ed25519.PrivateKey, method, path string, body []byte) (ts string, sigB58 string) {
	t.Helper()

	ts = clk.Now().UTC().Format(time.RFC3339)
	canonical := types.CanonicalAuthMessage(authPrefix, method, path, ts, body)
	sig := ed25519.Sign(metricsPriv, []byte(canonical))
	sigB58 = base58.Encode(sig)
	return ts, sigB58
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func goodUploadReq(t *testing.T, devicePK string, ts time.Time, kind string) types.UploadURLRequest {
	t.Helper()
	sum := sha256.Sum256([]byte("snapshot"))
	return types.UploadURLRequest{
		DevicePubkey:      devicePK,
		SnapshotTimestamp: ts.UTC().Format(time.RFC3339),
		SnapshotSHA256:    hex.EncodeToString(sum[:]),
		Kind:              kind,
	}
}

func TestTelemetry_StateIngest_Handler_UploadURL_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	h := &Handler{svcReady: func() bool { return true }}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.UploadURLPath, nil)

	h.uploadURLHandler(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, http.MethodPost, rr.Header().Get("Allow"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusMethodNotAllowed, er.Code)
	require.Contains(t, er.Error, "method not allowed")
}

func TestTelemetry_StateIngest_Handler_UploadURL_NotReady_Returns503WithRetryAfter(t *testing.T) {
	t.Parallel()

	cfg := Config{ServiceabilityRefreshInterval: 250 * time.Millisecond}
	_ = cfg.Validate()

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return false },
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(`{}`))

	h.uploadURLHandler(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Equal(t, "1", rr.Header().Get("Retry-After"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusServiceUnavailable, er.Code)
	require.Contains(t, er.Error, "not ready")
}

func TestTelemetry_StateIngest_Handler_UploadURL_NotReady_ShortCircuits(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))

	svc := NewServiceabilityView(slog.Default(), clk, 10*time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			t.Fatal("serviceability RPC should not be called")
			return nil, nil
		},
	})
	require.False(t, svc.Ready())

	authCalls := 0
	presignCalls := 0

	cfg := Config{
		Clock:                         clk,
		BucketName:                    "bucket-1",
		ServiceabilityRefreshInterval: 10 * time.Second,
		Presign: mockPresignClient{
			PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
				presignCalls++
				return nil, errors.New("should not be called")
			},
		},
		ServiceabilityRPC: mockServiceabilityRPC{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return nil, nil },
		},
	}
	require.NoError(t, cfg.Validate())

	h, err := NewHandler(slog.Default(), cfg, svc)
	require.NoError(t, err)

	origAuth := h.auth
	h.auth = &Authenticator{
		Clock: origAuth.Clock,
		Skew:  origAuth.Skew,
		LookupDevice: func(devicePK string) (serviceability.Device, bool) {
			authCalls++
			return serviceability.Device{}, false
		},
	}

	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	h.uploadURLHandler(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Equal(t, "10", rr.Header().Get("Retry-After"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusServiceUnavailable, er.Code)
	require.Contains(t, er.Error, "not ready")

	require.Equal(t, 0, authCalls)
	require.Equal(t, 0, presignCalls)
}

func TestTelemetry_StateIngest_Handler_UploadURL_BodyTooLarge_Returns413(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClock()
	cfg := Config{
		Clock:                         clk,
		MaxBodySize:                   8,
		ServiceabilityRefreshInterval: time.Second,
		Presign: mockPresignClient{PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return nil, errors.New("nope")
		}},
		BucketName: "b",
		ServiceabilityRPC: mockServiceabilityRPC{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		}},
	}
	require.NoError(t, cfg.Validate())

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return true },
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(`{"aaaaaaaaaaaaaaaaaaaaaaaa"}`))
	h.uploadURLHandler(rr, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusRequestEntityTooLarge, er.Code)
	require.Contains(t, er.Error, "request body too large")
}

func TestTelemetry_StateIngest_Handler_UploadURL_PresignError_Returns500(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	device, devicePK, metricsPriv := newTestDevice(t)

	svc := NewServiceabilityView(slog.Default(), clk, time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{device}}, nil
		},
	})
	require.NoError(t, svc.Refresh(context.Background()))

	presign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return nil, errors.New("boom")
		},
	}

	cfg := Config{
		Clock:             clk,
		Presign:           presign,
		BucketName:        "bucket-1",
		ServiceabilityRPC: svc.rpc,
	}
	require.NoError(t, cfg.Validate())

	h, err := NewHandler(slog.Default(), cfg, svc)
	require.NoError(t, err)

	reqStruct := goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex")
	reqBody := mustJSON(t, reqStruct)
	ts, sig := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, reqBody)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(string(reqBody)))
	req.Header.Set("X-DZ-Device", devicePK)
	req.Header.Set("X-DZ-Timestamp", ts)
	req.Header.Set("X-DZ-Signature", sig)

	h.uploadURLHandler(rr, req)
	require.Equal(t, http.StatusInternalServerError, rr.Code)

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusInternalServerError, er.Code)
	require.Contains(t, er.Error, "failed to create upload url")
}

func TestTelemetry_StateIngest_Handler_UploadURL_Success_ReturnsPresignedPut(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	device, devicePK, metricsPriv := newTestDevice(t)

	svc := NewServiceabilityView(slog.Default(), clk, time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{device}}, nil
		},
	})
	require.NoError(t, svc.Refresh(context.Background()))

	var gotBucket, gotKey, gotCT string
	presign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			gotBucket = aws.ToString(input.Bucket)
			gotKey = aws.ToString(input.Key)
			gotCT = aws.ToString(input.ContentType)
			return &awssigner.PresignedHTTPRequest{
				URL: "https://example.invalid/upload",
				SignedHeader: map[string][]string{
					"Host":         {"example.invalid"},
					"Content-Type": {"application/json"},
					"X-Test":       {"ok"},
				},
			}, nil
		},
	}

	cfg := Config{
		Clock:                         clk,
		Presign:                       presign,
		BucketName:                    "bucket-1",
		BucketPathPrefix:              "prefix",
		PresignTTL:                    10 * time.Minute,
		ServiceabilityRPC:             svc.rpc,
		ServiceabilityRefreshInterval: 30 * time.Second,
	}
	require.NoError(t, cfg.Validate())

	h, err := NewHandler(slog.Default(), cfg, svc)
	require.NoError(t, err)

	reqStruct := goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex")
	reqBody := mustJSON(t, reqStruct)

	ts, sig := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, reqBody)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DZ-Device", devicePK)
	req.Header.Set("X-DZ-Timestamp", ts)
	req.Header.Set("X-DZ-Signature", sig)

	h.uploadURLHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp types.UploadURLResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp.Status)
	require.Equal(t, http.MethodPut, resp.Upload.Method)
	require.Equal(t, "https://example.invalid/upload", resp.Upload.URL)
	require.Equal(t, "bucket-1", gotBucket)
	require.Equal(t, "application/json", gotCT)

	snapTS, err := time.Parse(time.RFC3339, reqStruct.SnapshotTimestamp)
	require.NoError(t, err)
	wantKey := buildSnapshotKey(cfg.BucketPathPrefix, reqStruct, snapTS.UTC())

	require.Equal(t, wantKey, resp.S3Key)
	require.Equal(t, wantKey, gotKey)
	require.NotContains(t, resp.Upload.Headers, "Host")
	require.Equal(t, []string{"application/json"}, resp.Upload.Headers["Content-Type"])

}

func TestTelemetry_StateIngest_Handler_UploadURL_Success_SetsPresignExpiryAndKey(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	device, devicePK, metricsPriv := newTestDevice(t)

	svc := NewServiceabilityView(slog.Default(), clk, time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{device}}, nil
		},
	})
	require.NoError(t, svc.Refresh(context.Background()))
	require.True(t, svc.Ready())

	var gotBucket, gotKey, gotCT string
	var gotExpires time.Duration

	presign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			gotBucket = aws.ToString(input.Bucket)
			gotKey = aws.ToString(input.Key)
			gotCT = aws.ToString(input.ContentType)
			var o s3.PresignOptions
			for _, fn := range opts {
				fn(&o)
			}
			gotExpires = o.Expires
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid/upload"}, nil
		},
	}

	cfg := Config{
		Clock:                         clk,
		Presign:                       presign,
		BucketName:                    "bucket-1",
		BucketPathPrefix:              "prefix",
		PresignTTL:                    7 * time.Minute,
		ServiceabilityRPC:             svc.rpc,
		ServiceabilityRefreshInterval: 30 * time.Second,
	}
	require.NoError(t, cfg.Validate())

	h, err := NewHandler(slog.Default(), cfg, svc)
	require.NoError(t, err)

	reqStruct := goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex")
	reqBody := mustJSON(t, reqStruct)

	ts, sig := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, reqBody)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, strings.NewReader(string(reqBody)))
	req.Header.Set("X-DZ-Device", devicePK)
	req.Header.Set("X-DZ-Timestamp", ts)
	req.Header.Set("X-DZ-Signature", sig)

	h.uploadURLHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	snapTS, err := time.Parse(time.RFC3339, reqStruct.SnapshotTimestamp)
	require.NoError(t, err)
	wantKey := buildSnapshotKey(cfg.BucketPathPrefix, reqStruct, snapTS.UTC())

	require.Equal(t, "bucket-1", gotBucket)
	require.Equal(t, wantKey, gotKey)
	require.Equal(t, "application/json", gotCT)
	require.Equal(t, cfg.PresignTTL, gotExpires)
}

func TestTelemetry_StateIngest_Handler_UploadURL_StatusAndMessage(t *testing.T) {
	t.Parallel()

	clk := clockwork.NewFakeClockAt(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	device, devicePK, metricsPriv := newTestDevice(t)

	svc := NewServiceabilityView(slog.Default(), clk, time.Second, mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{Devices: []serviceability.Device{device}}, nil
		},
	})
	require.NoError(t, svc.Refresh(context.Background()))
	require.True(t, svc.Ready())

	cfg := Config{
		Clock: clk,
		Presign: mockPresignClient{PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid/upload"}, nil
		}},
		BucketName:                    "bucket-1",
		ServiceabilityRPC:             svc.rpc,
		ServiceabilityRefreshInterval: 10 * time.Second,
	}
	require.NoError(t, cfg.Validate())

	h, err := NewHandler(slog.Default(), cfg, svc)
	require.NoError(t, err)

	type tc struct {
		name       string
		body       func() []byte
		setHeaders func(r *http.Request, body []byte)
		wantCode   int
		wantSubstr string
	}

	okBody := func() []byte {
		return mustJSON(t, goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex"))
	}

	signAndSet := func(r *http.Request, body []byte) {
		ts, sig := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, body)
		r.Header.Set("X-DZ-Device", devicePK)
		r.Header.Set("X-DZ-Timestamp", ts)
		r.Header.Set("X-DZ-Signature", sig)
	}

	tests := []tc{
		{
			name: "missing auth headers",
			body: okBody,
			setHeaders: func(r *http.Request, _ []byte) {
			},
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "missing auth headers",
		},
		{
			name: "invalid json",
			body: func() []byte { return []byte(`{"not":"closed"`) },
			setHeaders: func(r *http.Request, b []byte) {
				ts, sig := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, b)
				r.Header.Set("X-DZ-Device", devicePK)
				r.Header.Set("X-DZ-Timestamp", ts)
				r.Header.Set("X-DZ-Signature", sig)
			},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "invalid json",
		},
		{
			name: "missing fields",
			body: func() []byte {
				req := types.UploadURLRequest{
					DevicePubkey:      devicePK,
					SnapshotTimestamp: clk.Now().UTC().Format(time.RFC3339),
				}
				return mustJSON(t, req)
			},
			setHeaders: signAndSet,
			wantCode:   http.StatusBadRequest,
			wantSubstr: "missing fields",
		},
		{
			name: "device pubkey mismatch",
			body: func() []byte {
				_, otherPK, _ := newTestDevice(t)
				return mustJSON(t, goodUploadReq(t, otherPK, clk.Now(), "snmp-mib-ifmib-ifindex"))
			},
			setHeaders: signAndSet,
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "device pubkey mismatch",
		},
		{
			name: "invalid kind",
			body: func() []byte {
				return mustJSON(t, goodUploadReq(t, devicePK, clk.Now(), "nope-nope"))
			},
			setHeaders: signAndSet,
			wantCode:   http.StatusBadRequest,
			wantSubstr: "invalid kind",
		},
		{
			name: "invalid snapshot_sha256",
			body: func() []byte {
				req := goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex")
				req.SnapshotSHA256 = "zzzz"
				return mustJSON(t, req)
			},
			setHeaders: signAndSet,
			wantCode:   http.StatusBadRequest,
			wantSubstr: "invalid snapshot_sha256",
		},
		{
			name: "invalid snapshot_timestamp",
			body: func() []byte {
				req := goodUploadReq(t, devicePK, clk.Now(), "snmp-mib-ifmib-ifindex")
				req.SnapshotTimestamp = "not-rfc3339"
				return mustJSON(t, req)
			},
			setHeaders: signAndSet,
			wantCode:   http.StatusBadRequest,
			wantSubstr: "invalid snapshot_timestamp",
		},
		{
			name: "invalid timestamp header",
			body: okBody,
			setHeaders: func(r *http.Request, body []byte) {
				badTS := "yesterday"
				canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, types.UploadURLPath, badTS, body)
				sig := ed25519.Sign(metricsPriv, []byte(canonical))
				r.Header.Set("X-DZ-Device", devicePK)
				r.Header.Set("X-DZ-Timestamp", badTS)
				r.Header.Set("X-DZ-Signature", base58.Encode(sig))
			},
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "invalid timestamp",
		},
		{
			name: "timestamp outside skew",
			body: okBody,
			setHeaders: func(r *http.Request, body []byte) {
				ts := clk.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
				canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, types.UploadURLPath, ts, body)
				sig := ed25519.Sign(metricsPriv, []byte(canonical))
				r.Header.Set("X-DZ-Device", devicePK)
				r.Header.Set("X-DZ-Timestamp", ts)
				r.Header.Set("X-DZ-Signature", base58.Encode(sig))
			},
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "timestamp outside acceptable window",
		},
		{
			name: "invalid signature header",
			body: okBody,
			setHeaders: func(r *http.Request, body []byte) {
				ts, _ := signReq(t, types.AuthPrefixV1, clk, metricsPriv, http.MethodPost, types.UploadURLPath, body)
				r.Header.Set("X-DZ-Device", devicePK)
				r.Header.Set("X-DZ-Timestamp", ts)
				r.Header.Set("X-DZ-Signature", base58.Encode([]byte{1, 2, 3}))
			},
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "invalid signature encoding",
		},
		{
			name: "invalid signature",
			body: okBody,
			setHeaders: func(r *http.Request, body []byte) {
				_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				ts, sig := signReq(t, types.AuthPrefixV1, clk, wrongPriv, http.MethodPost, types.UploadURLPath, body)
				r.Header.Set("X-DZ-Device", devicePK)
				r.Header.Set("X-DZ-Timestamp", ts)
				r.Header.Set("X-DZ-Signature", sig)
			},
			wantCode:   http.StatusUnauthorized,
			wantSubstr: "invalid signature",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := tt.body()
			req := httptest.NewRequest(http.MethodPost, types.UploadURLPath, bytes.NewReader(body))
			rr := httptest.NewRecorder()

			if tt.setHeaders != nil {
				tt.setHeaders(req, body)
			}

			h.uploadURLHandler(rr, req)
			require.Equal(t, tt.wantCode, rr.Code)

			er := mustErrResp(t, rr)
			require.Equal(t, tt.wantCode, er.Code)
			require.Contains(t, er.Error, tt.wantSubstr)
		})
	}
}

func TestTelemetry_StateIngest_Handler_Healthz_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.HealthzPath, nil)

	h.healthzHandler(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, "GET, HEAD", rr.Header().Get("Allow"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusMethodNotAllowed, er.Code)
	require.Contains(t, er.Error, "method not allowed")
}

func TestTelemetry_StateIngest_Handler_Healthz_OK_GET(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.HealthzPath, nil)

	h.healthzHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "ok", body["status"])
}

func TestTelemetry_StateIngest_Handler_Healthz_OK_HEAD_EmptyBody(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, types.HealthzPath, nil)

	h.healthzHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Len(t, rr.Body.Bytes(), 0)
}

func TestTelemetry_StateIngest_Handler_Readyz_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	h := &Handler{svcReady: func() bool { return true }}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.ReadyzPath, nil)

	h.readyzHandler(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, "GET, HEAD", rr.Header().Get("Allow"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusMethodNotAllowed, er.Code)
	require.Contains(t, er.Error, "method not allowed")
}

func TestTelemetry_StateIngest_Handler_Readyz_NotReady_Returns503WithRetryAfter(t *testing.T) {
	t.Parallel()

	cfg := Config{ServiceabilityRefreshInterval: 250 * time.Millisecond}
	_ = cfg.Validate()

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return false },
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.ReadyzPath, nil)

	h.readyzHandler(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Equal(t, "1", rr.Header().Get("Retry-After"))
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "not_ready", body["status"])
}

func TestTelemetry_StateIngest_Handler_Readyz_Ready_Returns200(t *testing.T) {
	t.Parallel()

	cfg := Config{ServiceabilityRefreshInterval: 30 * time.Second}
	_ = cfg.Validate()

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return true },
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.ReadyzPath, nil)

	h.readyzHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Empty(t, rr.Header().Get("Retry-After"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "ready", body["status"])
}

func TestTelemetry_StateIngest_Handler_Readyz_Ready_HEAD_EmptyBody(t *testing.T) {
	t.Parallel()

	cfg := Config{ServiceabilityRefreshInterval: 30 * time.Second}
	_ = cfg.Validate()

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return true },
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, types.ReadyzPath, nil)

	h.readyzHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Len(t, rr.Body.Bytes(), 0)
}

func TestTelemetry_StateIngest_Handler_Readyz_NotReady_HEAD_EmptyBody(t *testing.T) {
	t.Parallel()

	cfg := Config{ServiceabilityRefreshInterval: 250 * time.Millisecond}
	_ = cfg.Validate()

	h := &Handler{
		cfg:      cfg,
		svcReady: func() bool { return false },
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, types.ReadyzPath, nil)

	h.readyzHandler(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Equal(t, "1", rr.Header().Get("Retry-After"))
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Len(t, rr.Body.Bytes(), 0)
}

func TestTelemetry_StateIngest_Handler_StateToCollect_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, types.StateToCollectPath, nil)

	h.stateToCollectHandler(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, http.MethodGet, rr.Header().Get("Allow"))

	er := mustErrResp(t, rr)
	require.Equal(t, http.StatusMethodNotAllowed, er.Code)
	require.Contains(t, er.Error, "method not allowed")
}

func TestTelemetry_StateIngest_Handler_StateToCollect_Success_ReturnsShowCommandsAndCustom(t *testing.T) {
	t.Parallel()

	showCommandsMap := map[string]string{
		"snmp-mib-ifmib-ifindex": "show snmp mib ifmib ifindex",
		"isis-database-detail":   "show isis database detail",
		"other-kind":             "show other command",
	}

	customKinds := []string{
		"custom-kind",
	}

	cfg := Config{
		StateToCollectShowCommands: showCommandsMap,
		StateToCollectCustom:       customKinds,
	}
	_ = cfg.Validate()

	h := &Handler{cfg: cfg}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.StateToCollectPath, nil)

	h.stateToCollectHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp types.StateToCollectResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.ShowCommands, len(showCommandsMap))
	require.Len(t, resp.Custom, len(customKinds))

	// Convert response to map for easier comparison
	respMap := make(map[string]string)
	for _, sc := range resp.ShowCommands {
		respMap[sc.Kind] = sc.Command
	}
	require.Equal(t, showCommandsMap, respMap)
	require.Equal(t, customKinds, resp.Custom)
}

func TestTelemetry_StateIngest_Handler_StateToCollect_UsesDefaultShowCommandsAndCustom(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Presign: mockPresignClient{PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return nil, nil
		}},
		BucketName: "test-bucket",
		ServiceabilityRPC: mockServiceabilityRPC{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return nil, nil
		}},
	}
	require.NoError(t, cfg.Validate())

	h := &Handler{cfg: cfg}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, types.StateToCollectPath, nil)

	h.stateToCollectHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp types.StateToCollectResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.ShowCommands, 2)
	require.Len(t, resp.Custom, 1)

	// Convert to map for order-independent comparison (map iteration is non-deterministic)
	respMap := make(map[string]string)
	for _, sc := range resp.ShowCommands {
		respMap[sc.Kind] = sc.Command
	}
	require.Equal(t, map[string]string{
		"snmp-mib-ifmib-ifindex": "show snmp mib ifmib ifindex",
		"isis-database-detail":   "show isis database detail",
	}, respMap)
	require.Equal(t, []string{"bgp-sockets"}, resp.Custom)
}
