package server

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/types"
	"github.com/mr-tron/base58"
)

var (
	ErrMissingAuthHeaders       = errors.New("missing auth headers")
	ErrInvalidTimestamp         = errors.New("invalid timestamp")
	ErrTimestampOutsideWindow   = errors.New("timestamp outside acceptable window")
	ErrDeviceNotAuthorized      = errors.New("device not authorized")
	ErrInvalidSignatureEncoding = errors.New("invalid signature encoding")
	ErrInvalidSignature         = errors.New("invalid signature")
)

type AuthContext struct {
	DevicePK string
	Device   serviceability.Device
	ClientTS time.Time
}

type Authenticator struct {
	Clock clockwork.Clock

	Skew time.Duration

	LookupDevice func(devicePK string) (serviceability.Device, bool)
}

func (a *Authenticator) Validate() error {
	if a.Clock == nil {
		return errors.New("clock is required")
	}
	if a.Skew <= 0 {
		return errors.New("skew must be > 0")
	}
	if a.LookupDevice == nil {
		return errors.New("lookup device function is required")
	}
	return nil
}

func (a *Authenticator) Authenticate(r *http.Request, body []byte) (*AuthContext, error) {
	devicePK := strings.TrimSpace(r.Header.Get("X-DZ-Device"))
	sigB58 := strings.TrimSpace(r.Header.Get("X-DZ-Signature"))
	tsHeader := strings.TrimSpace(r.Header.Get("X-DZ-Timestamp"))
	if devicePK == "" || sigB58 == "" || tsHeader == "" {
		return nil, ErrMissingAuthHeaders
	}

	clientTS, err := time.Parse(time.RFC3339, tsHeader)
	if err != nil {
		return nil, ErrInvalidTimestamp
	}

	now := a.Clock.Now()
	d := now.Sub(clientTS)
	if d < -a.Skew || d > a.Skew {
		return nil, ErrTimestampOutsideWindow
	}

	device, ok := a.LookupDevice(devicePK)
	if !ok {
		return nil, ErrDeviceNotAuthorized
	}

	sigBytes, err := base58.Decode(sigB58)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return nil, ErrInvalidSignatureEncoding
	}

	canonicalPath := types.CanonicalRequestPath(r)
	canonical := types.CanonicalAuthMessage(types.AuthPrefixV1, r.Method, canonicalPath, tsHeader, body)
	pub := ed25519.PublicKey(device.MetricsPublisherPubKey[:])
	if !ed25519.Verify(pub, []byte(canonical), sigBytes) {
		return nil, ErrInvalidSignature
	}

	return &AuthContext{
		DevicePK: devicePK,
		Device:   device,
		ClientTS: clientTS,
	}, nil
}
