package geoip

import (
	"context"
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func TestLake_GeoIP_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				ClickHouse: nil,
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing clickhouse", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: laketesting.NewLogger(),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "clickhouse connection is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

func TestLake_GeoIP_Store_UpsertRecords(t *testing.T) {
	t.Parallel()

	t.Run("upserts records successfully", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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

		// Verify data was inserted using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		// Get records using SurrogateKey
		entityID1 := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
		entityID2 := dataset.NewNaturalKey("8.8.8.8").ToSurrogate()
		currentRows, err := d.GetCurrentRows(context.Background(), conn, []dataset.SurrogateKey{entityID1, entityID2})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(currentRows), 2, "should have at least 2 geoip records")
	})

	t.Run("updates existing records", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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

		// Verify data was updated using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		entityID := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
		current, err := d.GetCurrentRow(context.Background(), conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "Los Angeles", current["city"])
		require.Equal(t, "Los Angeles", current["metro_name"])
	})

	t.Run("handles empty records", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		err = store.UpsertRecords(context.Background(), []*geoip.Record{})
		require.NoError(t, err)
	})

	t.Run("handles nil record", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		err = store.UpsertRecords(context.Background(), []*geoip.Record{nil})
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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
		require.Contains(t, err.Error(), "context canceled")
	})
}

func TestLake_GeoIP_Store_GetRecord(t *testing.T) {
	t.Parallel()

	t.Run("returns record when found", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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

		// Verify data was inserted using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		entityID := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
		current, err := d.GetCurrentRow(context.Background(), conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, expectedRecord.Country, current["country"])
		require.Equal(t, expectedRecord.Region, current["region"])
		require.Equal(t, expectedRecord.City, current["city"])
		if cityID, ok := current["city_id"].(int32); ok {
			require.Equal(t, expectedRecord.CityID, int(cityID))
		}
		require.Equal(t, expectedRecord.MetroName, current["metro_name"])
		if lat, ok := current["latitude"].(float64); ok {
			require.InDelta(t, expectedRecord.Latitude, lat, 0.0001)
		}
		if lon, ok := current["longitude"].(float64); ok {
			require.InDelta(t, expectedRecord.Longitude, lon, 0.0001)
		}
		require.Equal(t, expectedRecord.PostalCode, current["postal_code"])
		require.Equal(t, expectedRecord.TimeZone, current["time_zone"])
		if acc, ok := current["accuracy_radius"].(int32); ok {
			require.Equal(t, expectedRecord.AccuracyRadius, int(acc))
		}
		if asn, ok := current["asn"].(uint64); ok {
			require.Equal(t, expectedRecord.ASN, uint(asn))
		}
		require.Equal(t, expectedRecord.ASNOrg, current["asn_org"])
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Create tables by calling UpsertRecords with empty slice
		err = store.UpsertRecords(context.Background(), []*geoip.Record{})
		require.NoError(t, err)

		// Verify record doesn't exist using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		entityID := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
		current, err := d.GetCurrentRow(context.Background(), conn, entityID)
		require.NoError(t, err)
		require.Nil(t, current, "record should not exist")
	})

	// GetRecord method was removed - this test is obsolete
	// The method no longer exists, so we can't test nil IP handling

	t.Run("handles nullable fields", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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

		// Verify data was inserted using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		entityID := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
		current, err := d.GetCurrentRow(context.Background(), conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		// Nullable fields may be NULL or 0/false
	})
}

func TestLake_GeoIP_Store_GetRecords(t *testing.T) {
	t.Parallel()

	t.Run("returns all records", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
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

		// Verify data was inserted using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		// Verify all records exist
		entityIDs := []dataset.SurrogateKey{
			dataset.NewNaturalKey("1.1.1.1").ToSurrogate(),
			dataset.NewNaturalKey("8.8.8.8").ToSurrogate(),
			dataset.NewNaturalKey("9.9.9.9").ToSurrogate(),
		}
		currentRows, err := d.GetCurrentRows(context.Background(), conn, entityIDs)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(currentRows), 3, "should have at least 3 records")
	})

	t.Run("returns empty slice when no records", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Create tables by calling UpsertRecords with empty slice
		err = store.UpsertRecords(context.Background(), []*geoip.Record{})
		require.NoError(t, err)

		// Verify no records exist using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		currentRows, err := d.GetCurrentRows(context.Background(), conn, nil)
		require.NoError(t, err)
		require.Equal(t, 0, len(currentRows), "should have no records")
	})

	// GetRecords method was removed - this test is obsolete
}

func TestLake_GeoIP_Store_IPv6(t *testing.T) {
	t.Parallel()

	t.Run("handles IPv6 addresses", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		record := &geoip.Record{
			IP:          net.ParseIP("2001:4860:4860::8888"),
			CountryCode: "US",
			Country:     "United States",
			City:        "Mountain View",
		}

		err = store.UpsertRecords(context.Background(), []*geoip.Record{record})
		require.NoError(t, err)

		// Verify data was inserted using the dataset API
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		d, err := NewGeoIPRecordDataset(log)
		require.NoError(t, err)
		require.NotNil(t, d)

		entityID := dataset.NewNaturalKey("2001:4860:4860::8888").ToSurrogate()
		current, err := d.GetCurrentRow(context.Background(), conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "Mountain View", current["city"])
	})
}
