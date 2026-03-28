package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

// Mock source type for testing
type MockSource struct {
	ID   string
	Lat  float64
	Lng  float64
	Name string
}

// GetCoordinates implements the CoordinatesGetter interface
func (m MockSource) GetCoordinates() (latitude, longitude float64) {
	return m.Lat, m.Lng
}

func TestInternetLatency_Location_CalculateAndSortSourceDistances(t *testing.T) {
	t.Parallel()

	// Create mock sources
	sources := []CoordinatesGetter{
		MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},  // ~0 km from target
		MockSource{ID: "2", Lat: 51.5074, Lng: -0.1278, Name: "London"},     // ~5500 km from target
		MockSource{ID: "3", Lat: 35.6762, Lng: 139.6503, Name: "Tokyo"},     // ~10800 km from target
		MockSource{ID: "4", Lat: 40.7589, Lng: -73.9851, Name: "Manhattan"}, // ~5 km from target
	}

	targetLat := 40.7128 // New York coordinates
	targetLng := -74.0060

	result := CalculateAndSortSourceDistances(sources, targetLat, targetLng)

	// Verify we get all sources
	require.Len(t, result, len(sources))

	// Verify sources are sorted by distance (closest first)
	for i := 1; i < len(result); i++ {
		require.LessOrEqual(t, result[i-1].Distance, result[i].Distance,
			"Sources not sorted by distance at positions %d, %d", i-1, i)
	}

	// Verify the closest source is New York (distance should be ~0)
	closestSource := result[0].Source.(MockSource)
	require.Equal(t, "1", closestSource.ID)
	require.LessOrEqual(t, result[0].Distance, 1.0) // Should be very close to 0

	// Verify the farthest source is Tokyo
	farthestSource := result[len(result)-1].Source.(MockSource)
	require.Equal(t, "3", farthestSource.ID)
}

func TestInternetLatency_Location_CalculateAndSortSourceDistances_EmptyInput(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{}
	targetLat := 40.7128
	targetLng := -74.0060

	result := CalculateAndSortSourceDistances(sources, targetLat, targetLng)

	require.Empty(t, result)
}

func TestInternetLatency_Location_GetNearestSourcesSorted(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{
		MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},
		MockSource{ID: "2", Lat: 51.5074, Lng: -0.1278, Name: "London"},
		MockSource{ID: "3", Lat: 35.6762, Lng: 139.6503, Name: "Tokyo"},
		MockSource{ID: "4", Lat: 40.7589, Lng: -73.9851, Name: "Manhattan"},
		MockSource{ID: "5", Lat: 34.0522, Lng: -118.2437, Name: "Los Angeles"},
	}

	targetLat := 40.7128 // New York coordinates
	targetLng := -74.0060
	maxCount := 3

	result := GetNearestSourcesSorted(sources, targetLat, targetLng, maxCount)

	// Verify we get the requested number of sources
	require.Len(t, result, maxCount)

	// Verify the first source is the closest (New York)
	firstSource := result[0].(MockSource)
	require.Equal(t, "1", firstSource.ID)

	// Verify the second source is Manhattan (second closest)
	secondSource := result[1].(MockSource)
	require.Equal(t, "4", secondSource.ID)
}

func TestInternetLatency_Location_GetNearestSourcesSorted_EmptyInput(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{}
	targetLat := 40.7128
	targetLng := -74.0060
	maxCount := 5

	result := GetNearestSourcesSorted(sources, targetLat, targetLng, maxCount)

	require.Empty(t, result)
}

func TestInternetLatency_Location_GetNearestSourcesSorted_FewerSourcesThanRequested(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{
		MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},
		MockSource{ID: "2", Lat: 51.5074, Lng: -0.1278, Name: "London"},
	}

	targetLat := 40.7128
	targetLng := -74.0060
	maxCount := 5 // Request more than available

	result := GetNearestSourcesSorted(sources, targetLat, targetLng, maxCount)

	// Should return all available sources
	require.Len(t, result, len(sources))
}

func TestInternetLatency_Location_FilterSourcesByDistance(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{
		MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},  // ~0 km
		MockSource{ID: "2", Lat: 40.7589, Lng: -73.9851, Name: "Manhattan"}, // ~5 km
		MockSource{ID: "3", Lat: 51.5074, Lng: -0.1278, Name: "London"},     // ~5500 km (far)
		MockSource{ID: "4", Lat: 35.6762, Lng: 139.6503, Name: "Tokyo"},     // ~10800 km (far)
	}

	targetLat := 40.7128 // New York coordinates
	targetLng := -74.0060

	result := FilterSourcesByDistance(sources, targetLat, targetLng)

	expectedIDs := []string{"1", "2"} // New York and Manhattan
	require.Len(t, result, len(expectedIDs))

	for i, source := range result {
		sourceObj := source.(MockSource)
		require.Equal(t, expectedIDs[i], sourceObj.ID)
	}
}

func TestInternetLatency_Location_FilterSourcesByDistance_EmptyInput(t *testing.T) {
	t.Parallel()

	sources := []CoordinatesGetter{}
	targetLat := 40.7128
	targetLng := -74.0060

	result := FilterSourcesByDistance(sources, targetLat, targetLng)

	require.Empty(t, result)
}

func TestInternetLatency_Location_FilterSourcesByDistance_NoSourcesWithinRange(t *testing.T) {
	t.Parallel()

	// All sources are far from target
	sources := []CoordinatesGetter{
		MockSource{ID: "1", Lat: 51.5074, Lng: -0.1278, Name: "London"},
		MockSource{ID: "2", Lat: 35.6762, Lng: 139.6503, Name: "Tokyo"},
	}

	targetLat := 40.7128 // New York coordinates
	targetLng := -74.0060

	result := FilterSourcesByDistance(sources, targetLat, targetLng)

	require.Empty(t, result)
}

func TestInternetLatency_Location_CalculateDistanceToLocation(t *testing.T) {
	t.Parallel()

	// Create a test location
	location := LocationMatch{
		LocationCode: "New York",
		Latitude:     40.7128,
		Longitude:    -74.0060,
	}

	tests := []struct {
		name      string
		sourceLat float64
		sourceLng float64
		location  LocationMatch
		wantValid bool
		wantDist  float64 // Approximate expected distance
	}{
		{
			name:      "Same location",
			sourceLat: 40.7128,
			sourceLng: -74.0060,
			location:  location,
			wantValid: true,
			wantDist:  0.0,
		},
		{
			name:      "Manhattan (close)",
			sourceLat: 40.7589,
			sourceLng: -73.9851,
			location:  location,
			wantValid: true,
			wantDist:  5.0, // Approximately 5 km
		},
		{
			name:      "London (far)",
			sourceLat: 51.5074,
			sourceLng: -0.1278,
			location:  location,
			wantValid: true,
			wantDist:  5500.0, // Approximately 5500 km
		},
		{
			name:      "Invalid location (zero coordinates)",
			sourceLat: 40.7128,
			sourceLng: -74.0060,
			location: LocationMatch{
				LocationCode: "Invalid",
				Latitude:     0.0,
				Longitude:    0.0,
			},
			wantValid: false,
			wantDist:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateDistanceToLocation(tt.sourceLat, tt.sourceLng, tt.location)

			require.Equal(t, tt.wantValid, result.Valid)

			if tt.wantValid {
				// Allow for some tolerance in distance calculations
				tolerance := tt.wantDist * 0.1 // 10% tolerance
				if tolerance < 1.0 {
					tolerance = 1.0 // Minimum 1 km tolerance
				}

				require.InDelta(t, tt.wantDist, result.Distance, tolerance)
			} else {
				require.Equal(t, 0.0, result.Distance)
			}
		})
	}
}

func TestInternetLatency_Location_DistanceResult(t *testing.T) {
	t.Parallel()

	// Test DistanceResult struct
	result := DistanceResult{
		Distance: 123.45,
		Valid:    true,
	}

	require.Equal(t, 123.45, result.Distance)
	require.True(t, result.Valid)

	// Test invalid result
	invalidResult := DistanceResult{
		Distance: 0.0,
		Valid:    false,
	}

	require.Equal(t, 0.0, invalidResult.Distance)
	require.False(t, invalidResult.Valid)
}

func TestInternetLatency_Location_SourceDistance(t *testing.T) {
	t.Parallel()

	// Test SourceDistance struct
	mockSource := MockSource{ID: "test", Lat: 40.7128, Lng: -74.0060, Name: "Test"}
	sourceDistance := SourceDistance{
		Source:   mockSource,
		Distance: 123.45,
	}

	require.Equal(t, 123.45, sourceDistance.Distance)

	// Verify source can be cast back
	retrievedSource, ok := sourceDistance.Source.(MockSource)
	require.True(t, ok, "SourceDistance.Source should be castable to MockSource")
	require.Equal(t, "test", retrievedSource.ID)
}

// AlternativeSource is a test type to verify interface implementation
type AlternativeSource struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// GetCoordinates implements the CoordinatesGetter interface
func (a AlternativeSource) GetCoordinates() (latitude, longitude float64) {
	return a.Latitude, a.Longitude
}

func TestInternetLatency_Location_GenericFunctions_WithDifferentTypes(t *testing.T) {
	t.Parallel()

	// Test with different source types to verify generics work
	sources := []CoordinatesGetter{
		AlternativeSource{Name: "A", Latitude: 40.7128, Longitude: -74.0060},
		AlternativeSource{Name: "B", Latitude: 51.5074, Longitude: -0.1278},
	}

	// Test that functions work with alternative source type
	distances := CalculateAndSortSourceDistances(sources, 40.7128, -74.0060)
	require.Len(t, distances, 2)

	nearest := GetNearestSourcesSorted(sources, 40.7128, -74.0060, 1)
	require.Len(t, nearest, 1)

	filtered := FilterSourcesByDistance(sources, 40.7128, -74.0060)
	require.Len(t, filtered, 1) // Only the first source should be within range
}

func TestInternetLatency_Location_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("MaxCount zero", func(t *testing.T) {
		sources := []CoordinatesGetter{
			MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},
		}
		result := GetNearestSourcesSorted(sources, 40.7128, -74.0060, 0)
		require.Empty(t, result)
	})

	t.Run("MaxCount negative", func(t *testing.T) {
		sources := []CoordinatesGetter{
			MockSource{ID: "1", Lat: 40.7128, Lng: -74.0060, Name: "New York"},
		}
		result := GetNearestSourcesSorted(sources, 40.7128, -74.0060, -1)
		require.Empty(t, result)
	})

	t.Run("Extreme coordinates", func(t *testing.T) {
		sources := []CoordinatesGetter{
			MockSource{ID: "1", Lat: 90.0, Lng: 180.0, Name: "North Pole"},   // North pole
			MockSource{ID: "2", Lat: -90.0, Lng: -180.0, Name: "South Pole"}, // South pole
		}

		// Calculate distance from equator
		distances := CalculateAndSortSourceDistances(sources, 0.0, 0.0)
		require.Len(t, distances, 2)

		// Both poles should be roughly 10,000 km away (quarter of Earth's circumference)
		for _, distance := range distances {
			require.GreaterOrEqual(t, distance.Distance, 8000.0)
			require.LessOrEqual(t, distance.Distance, 12000.0)
		}
	})
}

func TestInternetLatency_Location_Integration(t *testing.T) {
	t.Parallel()

	// Integration test that demonstrates typical usage pattern
	sources := []CoordinatesGetter{
		MockSource{ID: "ny", Lat: 40.7128, Lng: -74.0060, Name: "New York"},
		MockSource{ID: "manhattan", Lat: 40.7589, Lng: -73.9851, Name: "Manhattan"},
		MockSource{ID: "brooklyn", Lat: 40.6782, Lng: -73.9442, Name: "Brooklyn"},
		MockSource{ID: "london", Lat: 51.5074, Lng: -0.1278, Name: "London"},
		MockSource{ID: "tokyo", Lat: 35.6762, Lng: 139.6503, Name: "Tokyo"},
	}

	targetLat := 40.7128 // New York coordinates
	targetLng := -74.0060

	// Step 1: Filter sources by distance
	nearSources := FilterSourcesByDistance(sources, targetLat, targetLng)

	// Should have 3 sources within 16km (NY, Manhattan, Brooklyn)
	require.Len(t, nearSources, 3)

	// Step 2: Get the 2 nearest sources
	nearest := GetNearestSourcesSorted(nearSources, targetLat, targetLng, 2)

	require.Len(t, nearest, 2)

	// Should be NY and Manhattan (in that order)
	firstSource := nearest[0].(MockSource)
	secondSource := nearest[1].(MockSource)

	require.Equal(t, "ny", firstSource.ID)
	require.Equal(t, "manhattan", secondSource.ID)

	// Step 3: Calculate detailed distance information
	distances := CalculateAndSortSourceDistances(nearSources, targetLat, targetLng)

	require.Len(t, distances, 3)

	// Verify distances are in ascending order
	for i := 1; i < len(distances); i++ {
		require.LessOrEqual(t, distances[i-1].Distance, distances[i].Distance)
	}
}

func TestInternetLatency_Location_HaversineDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lat1      float64
		lon1      float64
		lat2      float64
		lon2      float64
		expected  float64
		tolerance float64
	}{
		{
			name:      "Same point",
			lat1:      40.7128,
			lon1:      -74.0060,
			lat2:      40.7128,
			lon2:      -74.0060,
			expected:  0.0,
			tolerance: 0.1,
		},
		{
			name:      "New York to London",
			lat1:      40.7128,
			lon1:      -74.0060,
			lat2:      51.5074,
			lon2:      -0.1278,
			expected:  5585.0, // Approximately 5585 km
			tolerance: 50.0,   // 50km tolerance
		},
		{
			name:      "New York to Manhattan (very close)",
			lat1:      40.7128,
			lon1:      -74.0060,
			lat2:      40.7589,
			lon2:      -73.9851,
			expected:  5.2, // Approximately 5.2 km
			tolerance: 1.0, // 1km tolerance
		},
		{
			name:      "Equator to North Pole",
			lat1:      0.0,
			lon1:      0.0,
			lat2:      90.0,
			lon2:      0.0,
			expected:  10018.0, // Quarter of Earth's circumference
			tolerance: 100.0,   // 100km tolerance
		},
		{
			name:      "Antimeridian crossing",
			lat1:      0.0,
			lon1:      179.0,
			lat2:      0.0,
			lon2:      -179.0,
			expected:  222.0, // Should be shortest path across antimeridian
			tolerance: 50.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			distance := HaversineDistance(tt.lat1, tt.lon1, tt.lat2, tt.lon2)

			require.InDelta(t, tt.expected, distance, tt.tolerance,
				"HaversineDistance() = %f, want %f ± %f", distance, tt.expected, tt.tolerance)
		})
	}
}

func TestInternetLatency_Location_JSONLocation_Struct(t *testing.T) {
	t.Parallel()

	location := JSONLocation{
		Name:      "Test Location, US",
		Code:      "tst",
		Latitude:  40.7128,
		Longitude: -74.0060,
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(location)
	require.NoError(t, err, "Failed to marshal JSONLocation")

	// Test JSON unmarshaling
	var unmarshaled JSONLocation
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err, "Failed to unmarshal JSONLocation")

	require.Equal(t, location.Name, unmarshaled.Name, "Name mismatch")
	require.Equal(t, location.Code, unmarshaled.Code, "Code mismatch")
	require.Equal(t, location.Latitude, unmarshaled.Latitude, "Latitude mismatch")
	require.Equal(t, location.Longitude, unmarshaled.Longitude, "Longitude mismatch")
}

func TestInternetLatency_Location_LoadLocationsFromJSON(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Create a test JSON file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_locations.json")

	jsonContent := `[
  {"Name": "New York, NY, US", "Code": "nyc", "Latitude": 40.7128, "Longitude": -74.0060},
  {"Name": "London, UK", "Code": "lon", "Latitude": 51.5074, "Longitude": -0.1278},
  {"Name": "Tokyo, Japan", "Code": "tok", "Latitude": 35.6762, "Longitude": 139.6503}
]`

	err := os.WriteFile(testFile, []byte(jsonContent), 0644)
	require.NoError(t, err, "Failed to create test JSON file")

	locations, err := LoadLocationsFromJSON(log, testFile)
	require.NoError(t, err, "LoadLocationsFromJSON() failed")

	expectedLocations := []JSONLocation{
		{Name: "New York, NY, US", Code: "nyc", Latitude: 40.7128, Longitude: -74.0060},
		{Name: "London, UK", Code: "lon", Latitude: 51.5074, Longitude: -0.1278},
		{Name: "Tokyo, Japan", Code: "tok", Latitude: 35.6762, Longitude: 139.6503},
	}

	require.Len(t, locations, len(expectedLocations), "Unexpected number of locations")

	for i, expected := range expectedLocations {
		require.True(t, i < len(locations), "Missing location at index %d", i)

		actual := locations[i]
		require.Equal(t, expected.Name, actual.Name, "Location[%d].Name", i)
		require.Equal(t, expected.Code, actual.Code, "Location[%d].Code", i)
		require.InDelta(t, expected.Latitude, actual.Latitude, 0.0001, "Location[%d].Latitude", i)
		require.InDelta(t, expected.Longitude, actual.Longitude, 0.0001, "Location[%d].Longitude", i)
	}
}

func TestInternetLatency_Location_LoadLocationsFromJSON_EmptyArray(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_empty.json")

	jsonContent := `[]`

	err := os.WriteFile(testFile, []byte(jsonContent), 0644)
	require.NoError(t, err, "Failed to create test JSON file")

	_, err = LoadLocationsFromJSON(log, testFile)
	require.Error(t, err, "Expected error for empty JSON array")

	// Should be a CollectorError
	var collectorErr *CollectorError
	require.True(t, isCollectorErrorLocation(err, &collectorErr), "Error should be CollectorError, got %T", err)
}

func TestInternetLatency_Location_LoadLocationsFromJSON_InvalidData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		jsonContent string
		expectError bool
		expectEmpty bool
	}{
		{
			name:        "Invalid JSON",
			jsonContent: `{"invalid": "json"}`,
			expectError: true,
			expectEmpty: false,
		},
		{
			name:        "Missing code",
			jsonContent: `[{"Name": "Test", "Latitude": 40.0, "Longitude": -74.0}]`,
			expectError: true,
			expectEmpty: false,
		},
		{
			name:        "Missing name",
			jsonContent: `[{"Code": "tst", "Latitude": 40.0, "Longitude": -74.0}]`,
			expectError: true,
			expectEmpty: false,
		},
		{
			name:        "Invalid coordinates",
			jsonContent: `[{"Name": "Test", "Code": "tst", "Latitude": 0, "Longitude": 0}]`,
			expectError: true,
			expectEmpty: false,
		},
		{
			name:        "All invalid entries",
			jsonContent: `[{"Name": "", "Code": "tst"}, {"Name": "Test", "Code": ""}]`,
			expectError: true,
			expectEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			tempDir := t.TempDir()
			testFile := filepath.Join(tempDir, "test.json")

			err := os.WriteFile(testFile, []byte(tt.jsonContent), 0644)
			require.NoError(t, err, "Failed to create test JSON file")

			locations, err := LoadLocationsFromJSON(log, testFile)

			if tt.expectError {
				require.Error(t, err, "Expected error but got none")
			} else {
				require.NoError(t, err, "Unexpected error")
			}
			if tt.expectEmpty {
				require.Empty(t, locations, "Expected empty locations")
			}
		})
	}
}

func TestInternetLatency_Location_LoadLocationsFromJSON_NonexistentFile(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	_, err := LoadLocationsFromJSON(log, "nonexistent_file.json")
	require.Error(t, err, "Expected error for nonexistent file")

	require.ErrorContains(t, err, "no such file or directory")
}

func TestInternetLatency_Location_LoadLocationsFromJSON_SpecialCharacters(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Test JSON with special characters and quotes
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "special_chars.json")

	jsonContent := `[
  {"Name": "São Paulo, Brazil", "Code": "sao", "Latitude": -23.5505, "Longitude": -46.6333},
  {"Name": "Москва", "Code": "mow", "Latitude": 55.7558, "Longitude": 37.6176},
  {"Name": "日本、東京", "Code": "tok", "Latitude": 35.6762, "Longitude": 139.6503},
  {"Name": "Location with \"quotes\"", "Code": "loc", "Latitude": 40.0, "Longitude": 50.0}
]`

	err := os.WriteFile(testFile, []byte(jsonContent), 0644)
	require.NoError(t, err, "Failed to create test JSON file")

	locations, err := LoadLocationsFromJSON(log, testFile)
	require.NoError(t, err, "LoadLocationsFromJSON() failed")

	require.Len(t, locations, 4, "Expected 4 locations")

	// Verify special characters are preserved
	expectedNames := []string{
		"São Paulo, Brazil",
		"Москва",
		"日本、東京",
		"Location with \"quotes\"",
	}
	expectedCodes := []string{
		"sao",
		"mow",
		"tok",
		"loc",
	}

	for i, expectedName := range expectedNames {
		require.True(t, i < len(locations), "Missing location at index %d", i)
		require.Equal(t, expectedName, locations[i].Name, "Location[%d].Name", i)
		require.Equal(t, expectedCodes[i], locations[i].Code, "Location[%d].Code", i)
	}
}

// Integration test demonstrating typical workflow
func TestInternetLatency_Location_LocationWorkflow_Integration(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Create test JSON file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "locations.json")

	jsonContent := `[
  {"Name": "New York, NY, US", "Code": "nyc", "Latitude": 40.7128, "Longitude": -74.0060},
  {"Name": "London, UK", "Code": "lon", "Latitude": 51.5074, "Longitude": -0.1278},
  {"Name": "Tokyo, Japan", "Code": "tok", "Latitude": 35.6762, "Longitude": 139.6503},
  {"Name": "Sydney, Australia", "Code": "syd", "Latitude": -33.8688, "Longitude": 151.2093}
]`

	err := os.WriteFile(testFile, []byte(jsonContent), 0644)
	require.NoError(t, err, "Failed to create test JSON file")

	// Step 1: Load locations from JSON
	locations, err := LoadLocationsFromJSON(log, testFile)
	require.NoError(t, err, "LoadLocationsFromJSON() failed")

	require.Len(t, locations, 4, "Expected 4 locations")

	// Locations are already loaded and verified above

	// Step 4: Test distance calculations
	nyCoords := []float64{40.7128, -74.0060}
	londonCoords := []float64{51.5074, -0.1278}

	distance := HaversineDistance(nyCoords[0], nyCoords[1], londonCoords[0], londonCoords[1])

	// Should be approximately 5585 km
	require.GreaterOrEqual(t, distance, 5500.0, "Distance NY to London too small")
	require.LessOrEqual(t, distance, 5650.0, "Distance NY to London too large")
}

func TestInternetLatency_Location_GetLocations(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Since GetLocations calls the blockchain directly,
	// we can only test that it doesn't panic and returns a slice
	t.Run("Returns exchanges array without panic", func(t *testing.T) {
		ctx := t.Context()
		serviceabilityClient := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{{
						Code:   "nyc",
						Lat:    40.7128,
						Lng:    -74.0060,
						Status: serviceability.ExchangeStatusActivated,
					}},
				}, nil
			},
		}
		locations := GetLocations(ctx, log, serviceabilityClient)

		// Should return a slice (may be empty depending on blockchain state)
		require.NotNil(t, locations, "GetLocations() should return non-nil slice")
		require.Len(t, locations, 1, "GetLocations() should return one exchange")
		require.Equal(t, "nyc", locations[0].LocationCode)
	})
}

// Helper function to check if an error is a CollectorError
func isCollectorErrorLocation(err error, collectorErr **CollectorError) bool {
	if ce, ok := err.(*CollectorError); ok {
		*collectorErr = ce
		return true
	}
	return false
}
