package metrodb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTools_MaxMind_MetroDB_NewMetroDB_Success(t *testing.T) {
	t.Parallel()

	db, err := New()
	require.NoError(t, err)
	require.NotNil(t, db)
	require.NotEmpty(t, db.byKey)
}

func TestTools_MaxMind_MetroDB_Lookup_KnownValues(t *testing.T) {
	t.Parallel()

	db, err := New()
	require.NoError(t, err)

	tests := []struct {
		name       string
		city       string
		country    string
		wantMetro  string
		wantExists bool
	}{
		{
			name:       "Port Elizabeth ZA",
			city:       "Port Elizabeth",
			country:    "ZA",
			wantMetro:  "Port Elizabeth",
			wantExists: true,
		},
		{
			name:       "Sao Paulo BR",
			city:       "Sao Paulo",
			country:    "BR",
			wantMetro:  "São Paulo",
			wantExists: true,
		},
		{
			name:       "Frankfurt DE",
			city:       "Frankfurt am Main",
			country:    "DE",
			wantMetro:  "Frankfurt",
			wantExists: true,
		},
		{
			name:       "City of London GB",
			city:       "City of London",
			country:    "GB",
			wantMetro:  "London",
			wantExists: true,
		},
		{
			name:       "Tower Hamlets GB",
			city:       "Tower Hamlets",
			country:    "GB",
			wantMetro:  "London",
			wantExists: true,
		},
		{
			name:       "Unknown city",
			city:       "Nonexistent City",
			country:    "GB",
			wantMetro:  "",
			wantExists: false,
		},
		{
			name:       "Country code case-insensitive",
			city:       "Port Elizabeth",
			country:    "za",
			wantMetro:  "Port Elizabeth",
			wantExists: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMetro, ok := db.Lookup(tt.city, tt.country)
			require.Equal(t, tt.wantExists, ok)
			if tt.wantExists {
				require.Equal(t, tt.wantMetro, gotMetro)
			} else {
				require.Empty(t, gotMetro)
			}
		})
	}
}

func TestTools_MaxMind_MetroDB_Lookup_UnknownCountry(t *testing.T) {
	t.Parallel()

	db, err := New()
	require.NoError(t, err)

	metro, ok := db.Lookup("Port Elizabeth", "XX")
	require.False(t, ok)
	require.Empty(t, metro)
}

func TestTools_MaxMind_MetroDB_Lookup_NilReceiver(t *testing.T) {
	t.Parallel()

	var db *MetroDB
	metro, ok := db.Lookup("Port Elizabeth", "ZA")
	require.False(t, ok)
	require.Empty(t, metro)
}
