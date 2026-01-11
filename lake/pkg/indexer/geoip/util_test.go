package geoip

import (
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

type mockResolver struct {
	resolveFunc func(net.IP) *geoip.Record
}

var _ geoip.Resolver = (*mockResolver)(nil)

func (m *mockResolver) Resolve(ip net.IP) *geoip.Record {
	if m.resolveFunc != nil {
		return m.resolveFunc(ip)
	}
	return nil
}

func (m *mockResolver) MaybeResolveAddr(addr string) *geoip.Record {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	return m.Resolve(ip)
}

func TestLake_GeoIP_MaybeResolveAddr(t *testing.T) {
	t.Parallel()

	t.Run("resolves valid IPv4 address with port", func(t *testing.T) {
		t.Parallel()

		expectedRecord := &geoip.Record{
			IP:          net.ParseIP("1.1.1.1"),
			CountryCode: "US",
			Country:     "United States",
		}

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "1.1.1.1" {
					return expectedRecord
				}
				return nil
			},
		}

		result := MaybeResolveAddr(resolver, "1.1.1.1:80")
		require.NotNil(t, result)
		require.Equal(t, expectedRecord.IP.String(), result.IP.String())
		require.Equal(t, expectedRecord.CountryCode, result.CountryCode)
		require.Equal(t, expectedRecord.Country, result.Country)
	})

	t.Run("resolves valid IPv6 address with port", func(t *testing.T) {
		t.Parallel()

		expectedRecord := &geoip.Record{
			IP:          net.ParseIP("2001:4860:4860::8888"),
			CountryCode: "US",
			Country:     "United States",
		}

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "2001:4860:4860::8888" {
					return expectedRecord
				}
				return nil
			},
		}

		result := MaybeResolveAddr(resolver, "[2001:4860:4860::8888]:53")
		require.NotNil(t, result)
		require.Equal(t, expectedRecord.IP.String(), result.IP.String())
		require.Equal(t, expectedRecord.CountryCode, result.CountryCode)
	})

	t.Run("returns nil for invalid address format (no port)", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		result := MaybeResolveAddr(resolver, "1.1.1.1")
		require.Nil(t, result)
	})

	t.Run("returns nil for invalid IP in address", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		result := MaybeResolveAddr(resolver, "invalid-ip:80")
		require.Nil(t, result)
	})

	t.Run("returns nil when resolver returns nil", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return nil
			},
		}

		result := MaybeResolveAddr(resolver, "1.1.1.1:80")
		require.Nil(t, result)
	})

	t.Run("returns nil for empty address", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		result := MaybeResolveAddr(resolver, "")
		require.Nil(t, result)
	})

	t.Run("returns nil for malformed address", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		result := MaybeResolveAddr(resolver, "not:an:address:format")
		require.Nil(t, result)
	})

	t.Run("handles different port numbers", func(t *testing.T) {
		t.Parallel()

		expectedRecord := &geoip.Record{
			IP:          net.ParseIP("8.8.8.8"),
			CountryCode: "US",
		}

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "8.8.8.8" {
					return expectedRecord
				}
				return nil
			},
		}

		testCases := []string{
			"8.8.8.8:53",
			"8.8.8.8:443",
			"8.8.8.8:8080",
			"8.8.8.8:65535",
		}

		for _, addr := range testCases {
			result := MaybeResolveAddr(resolver, addr)
			require.NotNil(t, result, "should resolve address: %s", addr)
			require.Equal(t, "8.8.8.8", result.IP.String())
		}
	})

	t.Run("handles IPv6 with brackets", func(t *testing.T) {
		t.Parallel()

		expectedRecord := &geoip.Record{
			IP:          net.ParseIP("2001:4860:4860::8888"),
			CountryCode: "US",
		}

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "2001:4860:4860::8888" {
					return expectedRecord
				}
				return nil
			},
		}

		result := MaybeResolveAddr(resolver, "[2001:4860:4860::8888]:53")
		require.NotNil(t, result)
		require.Equal(t, "2001:4860:4860::8888", result.IP.String())
	})

	t.Run("handles IPv6 without brackets (should fail SplitHostPort)", func(t *testing.T) {
		t.Parallel()

		resolver := &mockResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		// IPv6 without brackets will fail SplitHostPort
		result := MaybeResolveAddr(resolver, "2001:4860:4860::8888:53")
		require.Nil(t, result)
	})
}
