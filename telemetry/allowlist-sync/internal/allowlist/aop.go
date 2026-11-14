package allowlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// AOPSetting is one hostname's per-hostname Authenticated Origin Pulls
// setting: the client certificate Cloudflare presents to that hostname's
// origin (spec section 4). gnmic accepts only a certificate our CA signed, so
// when it expires or AOP is turned off, every device drops.
type AOPSetting struct {
	// Set is false when the hostname has no setting, so Cloudflare presents
	// no certificate of ours.
	Set bool
	// Enabled is AOP turned on for the hostname.
	Enabled bool
	// Status is Cloudflare's status of the hostname's association with the
	// certificate, and CertStatus that of the certificate. Each is "active"
	// once deployed.
	Status     string
	CertStatus string
	ExpiresOn  time.Time
}

// Active is a setting Cloudflare presents a certificate for: AOP on, and the
// association and the certificate both active, not still deploying.
func (s AOPSetting) Active() bool {
	return s.Set && s.Enabled && s.Status == "active" && s.CertStatus == "active"
}

// AOPReader reads one hostname's AOP setting.
type AOPReader interface {
	AOPSetting(ctx context.Context, hostname string) (AOPSetting, error)
}

// cfAOPNotSet is Cloudflare's error code for a hostname with no AOP setting,
// which it answers with HTTP 404.
const cfAOPNotSet = 1553

// CloudflareAOP reads the zone's per-hostname AOP settings. The token needs
// "Zone > SSL and Certificates > Read" on the zone. The read returns the
// certificate, never its key.
type CloudflareAOP struct {
	Client  *http.Client
	BaseURL string // https://api.cloudflare.com/client/v4
	Token   string
	ZoneID  string
}

func (c *CloudflareAOP) AOPSetting(ctx context.Context, hostname string) (AOPSetting, error) {
	env, err := cfCall(ctx, c.Client, c.BaseURL, c.Token, http.MethodGet,
		fmt.Sprintf("/zones/%s/origin_tls_client_auth/hostnames/%s", c.ZoneID, url.PathEscape(hostname)), nil)
	var apiErr *cfAPIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound &&
		len(apiErr.Errors) == 1 && apiErr.Errors[0].Code == cfAOPNotSet {
		return AOPSetting{}, nil
	}
	if err != nil {
		return AOPSetting{}, fmt.Errorf("reading the AOP setting of %s: %w", hostname, err)
	}
	var r struct {
		Enabled    bool   `json:"enabled"`
		Status     string `json:"status"`
		CertStatus string `json:"cert_status"`
		ExpiresOn  string `json:"expires_on"`
	}
	if err := json.Unmarshal(env.Result, &r); err != nil {
		return AOPSetting{}, fmt.Errorf("decoding the AOP setting of %s: %w", hostname, err)
	}
	s := AOPSetting{Set: true, Enabled: r.Enabled, Status: r.Status, CertStatus: r.CertStatus}
	// A setting with no certificate yet, or one Cloudflare is still
	// processing, can have no expiry; it is not active either.
	if r.ExpiresOn != "" {
		if s.ExpiresOn, err = time.Parse(time.RFC3339, r.ExpiresOn); err != nil {
			return AOPSetting{}, fmt.Errorf("decoding the AOP setting of %s: expires_on %q: %w", hostname, r.ExpiresOn, err)
		}
	}
	return s, nil
}

// checkAOP reads each hostname's AOP setting and exports it. It runs after the
// pass, so a failed read never stops a pass, and a slow one delays the next by
// at most ReportTimeout. A hostname that cannot be read loses its series: the
// alert reads that as not reported, not as the last value read.
func (s *Syncer) checkAOP(ctx context.Context) {
	if s.cfg.AOP == nil {
		return
	}
	m := s.cfg.Metrics
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ReportTimeout)
	defer cancel()
	for _, h := range s.cfg.AOPHostnames {
		st, err := s.cfg.AOP.AOPSetting(ctx, h)
		if err != nil {
			m.Errors.WithLabelValues("aop").Inc()
			m.AOPActive.DeleteLabelValues(h)
			m.AOPExpiry.DeleteLabelValues(h)
			s.cfg.Log.Error("reading the AOP setting failed; its expiry is not reported", "hostname", h, "error", err)
			continue
		}
		active := 0.0
		if st.Active() {
			active = 1
		}
		m.AOPActive.WithLabelValues(h).Set(active)
		if st.ExpiresOn.IsZero() {
			m.AOPExpiry.DeleteLabelValues(h)
		} else {
			m.AOPExpiry.WithLabelValues(h).Set(unixOrZero(st.ExpiresOn))
		}
	}
}
