package geoip

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

type failingDB struct{}

func (f *failingDB) Exec(query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) Query(query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) QueryRow(query string, args ...any) *sql.Row {
	return &sql.Row{}
}

func (f *failingDB) Begin() (*sql.Tx, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) Close() error {
	return nil
}

func TestAI_MCP_GeoIP_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				DB: &failingDB{},
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing db", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "db is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

func TestAI_MCP_GeoIP_Store_CreateTablesIfNotExists(t *testing.T) {
	t.Parallel()

	t.Run("creates table", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Verify table exists by querying it
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM geoip_records").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("handles database error", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     &failingDB{},
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create table")
	})
}

func TestAI_MCP_GeoIP_Store_UpsertRecords(t *testing.T) {
	t.Parallel()

	t.Run("upserts records successfully", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		records := []*geoip.Record{
			{
				IP:                  net.ParseIP("1.1.1.1"),
				CountryCode:         "US",
				Country:             "United States",
				Region:              "California",
				City:                "San Francisco",
				CityID:              12345,
				MetroName:           "San Francisco",
				Latitude:            37.7749,
				Longitude:           -122.4194,
				PostalCode:          "94102",
				TimeZone:            "America/Los_Angeles",
				AccuracyRadius:      50,
				ASN:                 13335,
				ASNOrg:              "Cloudflare",
				IsAnycast:           true,
				IsAnonymousProxy:    false,
				IsSatelliteProvider: false,
			},
			{
				IP:                  net.ParseIP("8.8.8.8"),
				CountryCode:         "US",
				Country:             "United States",
				Region:              "California",
				City:                "Mountain View",
				CityID:              67890,
				MetroName:           "San Jose",
				Latitude:            37.4056,
				Longitude:           -122.0775,
				PostalCode:          "94043",
				TimeZone:            "America/Los_Angeles",
				AccuracyRadius:      100,
				ASN:                 15169,
				ASNOrg:              "Google",
				IsAnycast:           false,
				IsAnonymousProxy:    false,
				IsSatelliteProvider: false,
			},
		}

		err = store.UpsertRecords(context.Background(), records)
		require.NoError(t, err)

		// Verify records were inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM geoip_records").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("updates existing records", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Insert initial record
		initialRecord := &geoip.Record{
			IP:          net.ParseIP("1.1.1.1"),
			CountryCode: "US",
			Country:     "United States",
			City:        "San Francisco",
		}
		err = store.UpsertRecords(context.Background(), []*geoip.Record{initialRecord})
		require.NoError(t, err)

		// Update the record
		updatedRecord := &geoip.Record{
			IP:          net.ParseIP("1.1.1.1"),
			CountryCode: "US",
			Country:     "United States",
			City:        "Los Angeles",
			MetroName:   "Los Angeles",
		}
		err = store.UpsertRecords(context.Background(), []*geoip.Record{updatedRecord})
		require.NoError(t, err)

		// Verify only one record exists
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM geoip_records").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		// Verify record was updated
		record, err := store.GetRecord(net.ParseIP("1.1.1.1"))
		require.NoError(t, err)
		require.NotNil(t, record)
		require.Equal(t, "Los Angeles", record.City)
		require.Equal(t, "Los Angeles", record.MetroName)
	})

	t.Run("handles empty records", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		err = store.UpsertRecords(context.Background(), []*geoip.Record{})
		require.NoError(t, err)
	})

	t.Run("handles nil record", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		err = store.UpsertRecords(context.Background(), []*geoip.Record{nil})
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		records := []*geoip.Record{
			{
				IP:          net.ParseIP("1.1.1.1"),
				CountryCode: "US",
			},
		}

		err = store.UpsertRecords(ctx, records)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestAI_MCP_GeoIP_Store_GetRecord(t *testing.T) {
	t.Parallel()

	t.Run("returns record when found", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		expectedRecord := &geoip.Record{
			IP:                  net.ParseIP("1.1.1.1"),
			CountryCode:         "US",
			Country:             "United States",
			Region:              "California",
			City:                "San Francisco",
			CityID:              12345,
			MetroName:           "San Francisco",
			Latitude:            37.7749,
			Longitude:           -122.4194,
			PostalCode:          "94102",
			TimeZone:            "America/Los_Angeles",
			AccuracyRadius:      50,
			ASN:                 13335,
			ASNOrg:              "Cloudflare",
			IsAnycast:           true,
			IsAnonymousProxy:    false,
			IsSatelliteProvider: false,
		}

		err = store.UpsertRecords(context.Background(), []*geoip.Record{expectedRecord})
		require.NoError(t, err)

		record, err := store.GetRecord(net.ParseIP("1.1.1.1"))
		require.NoError(t, err)
		require.NotNil(t, record)
		require.Equal(t, expectedRecord.IP.String(), record.IP.String())
		require.Equal(t, expectedRecord.CountryCode, record.CountryCode)
		require.Equal(t, expectedRecord.Country, record.Country)
		require.Equal(t, expectedRecord.Region, record.Region)
		require.Equal(t, expectedRecord.City, record.City)
		require.Equal(t, expectedRecord.CityID, record.CityID)
		require.Equal(t, expectedRecord.MetroName, record.MetroName)
		require.InDelta(t, expectedRecord.Latitude, record.Latitude, 0.0001)
		require.InDelta(t, expectedRecord.Longitude, record.Longitude, 0.0001)
		require.Equal(t, expectedRecord.PostalCode, record.PostalCode)
		require.Equal(t, expectedRecord.TimeZone, record.TimeZone)
		require.Equal(t, expectedRecord.AccuracyRadius, record.AccuracyRadius)
		require.Equal(t, expectedRecord.ASN, record.ASN)
		require.Equal(t, expectedRecord.ASNOrg, record.ASNOrg)
		require.Equal(t, expectedRecord.IsAnycast, record.IsAnycast)
		require.Equal(t, expectedRecord.IsAnonymousProxy, record.IsAnonymousProxy)
		require.Equal(t, expectedRecord.IsSatelliteProvider, record.IsSatelliteProvider)
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		record, err := store.GetRecord(net.ParseIP("1.1.1.1"))
		require.NoError(t, err)
		require.Nil(t, record)
	})

	t.Run("returns error when ip is nil", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		record, err := store.GetRecord(nil)
		require.Error(t, err)
		require.Nil(t, record)
		require.Contains(t, err.Error(), "ip is nil")
	})

	t.Run("handles nullable fields", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Insert record with minimal fields (nullable fields will be NULL)
		record := &geoip.Record{
			IP:          net.ParseIP("1.1.1.1"),
			CountryCode: "US",
			Country:     "United States",
			// CityID, AccuracyRadius, ASN, booleans will be 0/false
		}

		err = store.UpsertRecords(context.Background(), []*geoip.Record{record})
		require.NoError(t, err)

		retrieved, err := store.GetRecord(net.ParseIP("1.1.1.1"))
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, 0, retrieved.CityID)
		require.Equal(t, 0, retrieved.AccuracyRadius)
		require.Equal(t, uint(0), retrieved.ASN)
		require.False(t, retrieved.IsAnycast)
		require.False(t, retrieved.IsAnonymousProxy)
		require.False(t, retrieved.IsSatelliteProvider)
	})
}

func TestAI_MCP_GeoIP_Store_GetRecords(t *testing.T) {
	t.Parallel()

	t.Run("returns all records", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		records := []*geoip.Record{
			{
				IP:          net.ParseIP("1.1.1.1"),
				CountryCode: "US",
				Country:     "United States",
				City:        "San Francisco",
			},
			{
				IP:          net.ParseIP("8.8.8.8"),
				CountryCode: "US",
				Country:     "United States",
				City:        "Mountain View",
			},
			{
				IP:          net.ParseIP("9.9.9.9"),
				CountryCode: "US",
				Country:     "United States",
				City:        "Reston",
			},
		}

		err = store.UpsertRecords(context.Background(), records)
		require.NoError(t, err)

		allRecords, err := store.GetRecords()
		require.NoError(t, err)
		require.Len(t, allRecords, 3)

		// Verify we can find all records
		ipMap := make(map[string]*geoip.Record)
		for _, r := range allRecords {
			ipMap[r.IP.String()] = r
		}

		require.Contains(t, ipMap, "1.1.1.1")
		require.Contains(t, ipMap, "8.8.8.8")
		require.Contains(t, ipMap, "9.9.9.9")
	})

	t.Run("returns empty slice when no records", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		records, err := store.GetRecords()
		require.NoError(t, err)
		require.NotNil(t, records)
		require.Len(t, records, 0)
	})

	t.Run("handles database error", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     &failingDB{},
		})
		require.NoError(t, err)

		records, err := store.GetRecords()
		require.Error(t, err)
		require.Nil(t, records)
		require.Contains(t, err.Error(), "failed to query records")
	})
}

func TestAI_MCP_GeoIP_Store_IPv6(t *testing.T) {
	t.Parallel()

	t.Run("handles IPv6 addresses", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		record := &geoip.Record{
			IP:          net.ParseIP("2001:4860:4860::8888"),
			CountryCode: "US",
			Country:     "United States",
			City:        "Mountain View",
		}

		err = store.UpsertRecords(context.Background(), []*geoip.Record{record})
		require.NoError(t, err)

		retrieved, err := store.GetRecord(net.ParseIP("2001:4860:4860::8888"))
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, "2001:4860:4860::8888", retrieved.IP.String())
		require.Equal(t, "Mountain View", retrieved.City)
	})
}
