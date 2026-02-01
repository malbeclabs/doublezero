package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_StateIngest_Authenticator_Validate(t *testing.T) {
	t.Parallel()

	type tc struct {
		name    string
		a       *Authenticator
		wantErr string
	}

	okLookup := func(string) (serviceability.Device, bool) { return serviceability.Device{}, true }

	newAuth := func(clk clockwork.Clock, lookup func(string) (serviceability.Device, bool)) *Authenticator {
		return &Authenticator{Clock: clk, Skew: time.Minute, LookupDevice: lookup}
	}

	tests := []tc{
		{
			name: "ok",
			a:    newAuth(clockwork.NewFakeClock(), okLookup),
		},
		{
			name: "missing clock",
			a: &Authenticator{
				Skew:         time.Minute,
				LookupDevice: okLookup,
			},
			wantErr: "clock is required",
		},
		{
			name: "invalid skew",
			a: &Authenticator{
				Clock:        clockwork.NewFakeClock(),
				Skew:         0,
				LookupDevice: okLookup,
			},
			wantErr: "skew must be > 0",
		},
		{
			name: "missing lookup",
			a: &Authenticator{
				Clock: clockwork.NewFakeClock(),
				Skew:  time.Minute,
			},
			wantErr: "lookup device function is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.a.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestTelemetry_StateIngest_Authenticator_Authenticate(t *testing.T) {
	t.Parallel()

	makeDevice := func(pub ed25519.PublicKey) serviceability.Device {
		var d serviceability.Device
		copy(d.MetricsPublisherPubKey[:], pub)
		return d
	}

	makeReq := func(t *testing.T, rawURL, devicePK, sigB58, ts string) *http.Request {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, rawURL, nil)
		require.NoError(t, err)
		if devicePK != "" {
			req.Header.Set("X-DZ-Device", devicePK)
		}
		if sigB58 != "" {
			req.Header.Set("X-DZ-Signature", sigB58)
		}
		if ts != "" {
			req.Header.Set("X-DZ-Timestamp", ts)
		}
		return req
	}

	newAuth := func(clk clockwork.Clock, lookup func(string) (serviceability.Device, bool)) *Authenticator {
		return &Authenticator{Clock: clk, Skew: time.Minute, LookupDevice: lookup}
	}

	fixedNow := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("missing auth headers", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		a := newAuth(clk, func(string) (serviceability.Device, bool) { return serviceability.Device{}, true })
		require.NoError(t, a.Validate())

		req := makeReq(t, "http://example/v1/snapshots/upload-url", "", "", "")
		_, err := a.Authenticate(req, []byte(`{}`))
		require.ErrorIs(t, err, ErrMissingAuthHeaders)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		a := newAuth(clk, func(string) (serviceability.Device, bool) { return serviceability.Device{}, true })
		require.NoError(t, a.Validate())

		req := makeReq(t, "http://example/v1/snapshots/upload-url", "devpk", "sig", "not-a-time")
		_, err := a.Authenticate(req, []byte(`{}`))
		require.ErrorIs(t, err, ErrInvalidTimestamp)
	})

	t.Run("timestamp outside window", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		a := newAuth(clk, func(string) (serviceability.Device, bool) { return serviceability.Device{}, true })
		require.NoError(t, a.Validate())

		ts := clk.Now().Add(-2 * time.Minute).Format(time.RFC3339)
		req := makeReq(t, "http://example/v1/snapshots/upload-url", "devpk", "sig", ts)
		_, err := a.Authenticate(req, []byte(`{}`))
		require.ErrorIs(t, err, ErrTimestampOutsideWindow)
	})

	t.Run("device not authorized", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		a := newAuth(clk, func(string) (serviceability.Device, bool) { return serviceability.Device{}, false })
		require.NoError(t, a.Validate())

		ts := clk.Now().Format(time.RFC3339)
		req := makeReq(t, "http://example/v1/snapshots/upload-url", "devpk", "sig", ts)
		_, err := a.Authenticate(req, []byte(`{}`))
		require.ErrorIs(t, err, ErrDeviceNotAuthorized)
	})

	t.Run("invalid signature encoding", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		a := newAuth(clk, func(string) (serviceability.Device, bool) { return makeDevice(pub), true })
		require.NoError(t, a.Validate())

		ts := clk.Now().Format(time.RFC3339)
		req := makeReq(t, "http://example/v1/snapshots/upload-url", "devpk", "%%%not-base58%%%", ts)
		_, gotErr := a.Authenticate(req, []byte(`{}`))
		require.ErrorIs(t, gotErr, ErrInvalidSignatureEncoding)
	})

	t.Run("invalid signature wrong body", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		a := newAuth(clk, func(string) (serviceability.Device, bool) { return makeDevice(pub), true })
		require.NoError(t, a.Validate())

		ts := clk.Now().UTC().Format(time.RFC3339)
		body := []byte(`{"x":1}`)
		wrongBody := []byte(`{"x":2}`)

		req := makeReq(t, "http://example/v1/snapshots/upload-url", "devpk", "", ts)

		canonicalPath := req.URL.EscapedPath()
		canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, canonicalPath, ts, wrongBody)
		sig := ed25519.Sign(priv, []byte(canonical))
		req.Header.Set("X-DZ-Signature", base58.Encode(sig))

		_, gotErr := a.Authenticate(req, body)
		require.ErrorIs(t, gotErr, ErrInvalidSignature)
	})

	t.Run("ok", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var lookedUp string
		devicePK := "device-pk-123"
		d := makeDevice(pub)

		a := newAuth(clk, func(pk string) (serviceability.Device, bool) {
			lookedUp = pk
			return d, true
		})
		require.NoError(t, a.Validate())

		body := []byte(`{"hello":"world"}`)
		ts := clk.Now().UTC().Format(time.RFC3339)

		req := makeReq(t, "http://example/v1/snapshots/upload-url", devicePK, "", ts)
		canonicalPath := req.URL.EscapedPath()
		canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, canonicalPath, ts, body)
		sig := ed25519.Sign(priv, []byte(canonical))
		req.Header.Set("X-DZ-Signature", base58.Encode(sig))

		ctx, gotErr := a.Authenticate(req, body)
		require.NoError(t, gotErr)
		require.NotNil(t, ctx)
		require.Equal(t, devicePK, ctx.DevicePK)
		require.Equal(t, devicePK, lookedUp)
		require.True(t, ctx.ClientTS.Equal(clk.Now()))
	})

	t.Run("path canonicalization option A: double slash must be signed as-is", func(t *testing.T) {
		t.Parallel()

		clk := clockwork.NewFakeClockAt(fixedNow)
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		a := newAuth(clk, func(string) (serviceability.Device, bool) { return makeDevice(pub), true })
		require.NoError(t, a.Validate())

		body := []byte(`{"hello":"world"}`)
		ts := clk.Now().UTC().Format(time.RFC3339)

		req := makeReq(t, "http://example//v1/snapshots/upload-url", "devpk", "", ts)

		canonicalPath := req.URL.EscapedPath()
		require.Equal(t, "//v1/snapshots/upload-url", canonicalPath)

		canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, http.MethodPost, canonicalPath, ts, body)
		sig := ed25519.Sign(priv, []byte(canonical))
		req.Header.Set("X-DZ-Signature", base58.Encode(sig))

		_, gotErr := a.Authenticate(req, body)
		require.NoError(t, gotErr)
	})
}
