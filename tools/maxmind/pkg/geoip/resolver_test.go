package geoip

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/oschwald/geoip2-golang"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
)

func TestGlobalMonitor_GeoIP_Resolve_WithGeneratedMMDBs(t *testing.T) {
	t.Parallel()

	const cidr = "1.1.1.0/24"
	const ipStr = "1.1.1.1"

	cityPath := writeMMDB(t, "city.mmdb", "GeoLite2-City", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"country": mmdbtype.Map{
				"iso_code": mmdbtype.String("CA"),
				"names":    mmdbtype.Map{"en": mmdbtype.String("Canada")},
			},
			"subdivisions": mmdbtype.Slice{
				mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String("Ontario")}},
			},
			"city": mmdbtype.Map{
				"geoname_id": mmdbtype.Uint32(123),
				"names":      mmdbtype.Map{"en": mmdbtype.String("Ottawa")},
			},
			"postal": mmdbtype.Map{
				"code": mmdbtype.String("K1A 0A6"),
			},
			"location": mmdbtype.Map{
				"latitude":        mmdbtype.Float64(45.4215),
				"longitude":       mmdbtype.Float64(-75.6972),
				"time_zone":       mmdbtype.String("America/Toronto"),
				"accuracy_radius": mmdbtype.Uint16(50),
			},
			"traits": mmdbtype.Map{
				"is_anycast":            mmdbtype.Bool(false),
				"is_anonymous_proxy":    mmdbtype.Bool(false),
				"is_satellite_provider": mmdbtype.Bool(false),
			},
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})

	asnPath := writeMMDB(t, "asn.mmdb", "GeoLite2-ASN", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"autonomous_system_number":       mmdbtype.Uint32(64500),
			"autonomous_system_organization": mmdbtype.String("ExampleNet"),
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})

	cityDB := openGeoIP(t, cityPath)
	asnDB := openGeoIP(t, asnPath)

	metro := &metrodb.MetroDB{} // no fixture loaded -> Lookup should fail -> "Unknown"
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r, err := NewResolver(log, cityDB, asnDB, metro)
	require.NoError(t, err)

	got := r.Resolve(net.ParseIP(ipStr))
	require.NotNil(t, got)

	require.Equal(t, "CA", got.CountryCode)
	require.Equal(t, "Canada", got.Country)
	require.Equal(t, "Ontario", got.Region)
	require.Equal(t, "Ottawa", got.City)
	require.Equal(t, 123, got.CityID)
	require.InDelta(t, 45.4215, got.Latitude, 1e-9)
	require.InDelta(t, -75.6972, got.Longitude, 1e-9)
	require.Equal(t, "K1A 0A6", got.PostalCode)
	require.Equal(t, "America/Toronto", got.TimeZone)
	require.Equal(t, 50, got.AccuracyRadius)
	require.Equal(t, uint(64500), got.ASN)
	require.Equal(t, "ExampleNet", got.ASNOrg)
	require.False(t, got.IsAnycast)
	require.False(t, got.IsAnonymousProxy)
	require.False(t, got.IsSatelliteProvider)
	require.Equal(t, "Unknown", got.MetroName)
}

func TestGlobalMonitor_GeoIP_Resolve_NilIP(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r, err := NewResolver(log, &geoip2.Reader{}, &geoip2.Reader{}, &metrodb.MetroDB{})
	require.NoError(t, err)

	require.Nil(t, r.Resolve(nil))
}

func TestGlobalMonitor_GeoIP_Resolve_CityOnly(t *testing.T) {
	t.Parallel()

	const cidr, ipStr = "1.1.1.0/24", "1.1.1.1"

	cityPath := writeMMDB(t, "city.mmdb", "GeoLite2-City", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"country": mmdbtype.Map{
				"iso_code": mmdbtype.String("CA"),
				"names":    mmdbtype.Map{"en": mmdbtype.String("Canada")},
			},
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})
	cityDB := openGeoIP(t, cityPath)

	r := &resolver{
		log:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		cityDB:  cityDB,
		asnDB:   nil,
		metroDB: &metrodb.MetroDB{},
	}

	got := r.Resolve(net.ParseIP(ipStr))
	require.NotNil(t, got)
	require.Equal(t, "CA", got.CountryCode)
	require.Equal(t, "Canada", got.Country)
	require.Zero(t, got.ASN)
	require.Equal(t, "Unknown", got.MetroName)
}

func TestGlobalMonitor_GeoIP_Resolve_ASNOnly(t *testing.T) {
	t.Parallel()

	const cidr, ipStr = "1.1.1.0/24", "1.1.1.1"

	asnPath := writeMMDB(t, "asn.mmdb", "GeoLite2-ASN", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"autonomous_system_number":       mmdbtype.Uint32(64501),
			"autonomous_system_organization": mmdbtype.String("OnlyASN"),
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})
	asnDB := openGeoIP(t, asnPath)

	r := &resolver{
		log:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		cityDB:  nil,
		asnDB:   asnDB,
		metroDB: &metrodb.MetroDB{},
	}

	got := r.Resolve(net.ParseIP(ipStr))
	require.NotNil(t, got)
	require.Equal(t, uint(64501), got.ASN)
	require.Equal(t, "OnlyASN", got.ASNOrg)
	require.Empty(t, got.Country)
	require.Equal(t, "Unknown", got.MetroName)
}

func TestGlobalMonitor_GeoIP_Resolve_NotFound(t *testing.T) {
	t.Parallel()

	cityPath := writeMMDB(t, "city.mmdb", "GeoLite2-City", func(w *mmdbwriter.Tree) {
		require.NoError(t, w.Insert(
			mustCIDR(t, "1.1.1.0/24"),
			mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String("CA")}},
		))
	})

	asnPath := writeMMDB(t, "asn.mmdb", "GeoLite2-ASN", func(w *mmdbwriter.Tree) {
		require.NoError(t, w.Insert(
			mustCIDR(t, "1.1.1.0/24"),
			mmdbtype.Map{"autonomous_system_number": mmdbtype.Uint32(64500)},
		))
	})

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r, err := NewResolver(
		log,
		openGeoIP(t, cityPath),
		openGeoIP(t, asnPath),
		&metrodb.MetroDB{},
	)
	require.NoError(t, err)

	require.Nil(t, r.Resolve(net.ParseIP("1.1.2.1")))
}

func TestGlobalMonitor_GeoIP_Resolve_NoReaders(t *testing.T) {
	t.Parallel()

	r := &resolver{
		log:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		cityDB:  nil,
		asnDB:   nil,
		metroDB: &metrodb.MetroDB{},
	}

	require.Nil(t, r.Resolve(net.ParseIP("1.1.1.1")))
}

func TestGlobalMonitor_GeoIP_Resolve_WithTraits(t *testing.T) {
	t.Parallel()

	const cidr = "2.2.2.0/24"
	const ipStr = "2.2.2.2"

	cityPath := writeMMDB(t, "city.mmdb", "GeoLite2-City", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"country": mmdbtype.Map{
				"iso_code": mmdbtype.String("US"),
				"names":    mmdbtype.Map{"en": mmdbtype.String("United States")},
			},
			"location": mmdbtype.Map{
				"latitude":  mmdbtype.Float64(37.7749),
				"longitude": mmdbtype.Float64(-122.4194),
			},
			"traits": mmdbtype.Map{
				"is_anycast":            mmdbtype.Bool(true),
				"is_anonymous_proxy":    mmdbtype.Bool(true),
				"is_satellite_provider": mmdbtype.Bool(true),
			},
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})

	asnPath := writeMMDB(t, "asn.mmdb", "GeoLite2-ASN", func(w *mmdbwriter.Tree) {
		rec := mmdbtype.Map{
			"autonomous_system_number":       mmdbtype.Uint32(12345),
			"autonomous_system_organization": mmdbtype.String("Test ASN Org"),
		}
		require.NoError(t, w.Insert(mustCIDR(t, cidr), rec))
	})

	cityDB := openGeoIP(t, cityPath)
	asnDB := openGeoIP(t, asnPath)

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r, err := NewResolver(log, cityDB, asnDB, &metrodb.MetroDB{})
	require.NoError(t, err)

	got := r.Resolve(net.ParseIP(ipStr))
	require.NotNil(t, got)

	require.True(t, got.IsAnycast)
	require.True(t, got.IsAnonymousProxy)
	require.True(t, got.IsSatelliteProvider)
}

func writeMMDB(t *testing.T, filename, dbType string, inserts func(w *mmdbwriter.Tree)) string {
	t.Helper()
	w, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: dbType, RecordSize: 24})
	require.NoError(t, err)
	inserts(w)

	dir := t.TempDir()
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, err = w.WriteTo(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return path
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return n
}

func openGeoIP(t *testing.T, path string) *geoip2.Reader {
	t.Helper()
	r, err := geoip2.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}
