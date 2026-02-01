package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

const MaxDistanceKM = 60.0

type LocationMatch struct {
	LocationCode string
	Latitude     float64
	Longitude    float64
}

type JSONLocation struct {
	Name      string  `json:"Name"` // Full name like "Amsterdam, NL"
	Code      string  `json:"Code"` // Short code like "ams"
	Latitude  float64 `json:"Latitude"`
	Longitude float64 `json:"Longitude"`
}

// CoordinatesGetter interface for types that have geographic coordinates
type CoordinatesGetter interface {
	GetCoordinates() (latitude, longitude float64)
}

// SourceDistance represents a generic source/probe with its distance to a location
type SourceDistance struct {
	Source   CoordinatesGetter
	Distance float64
}

// Using generic type T so the first argument can accept both ripeatlas probes and wheresitup sources
func CalculateAndSortSourceDistances[T CoordinatesGetter](sources []T, targetLat, targetLng float64) []SourceDistance {
	var sourceDistances []SourceDistance
	for _, source := range sources {
		sourceLat, sourceLng := source.GetCoordinates()
		distance := HaversineDistance(targetLat, targetLng, sourceLat, sourceLng)
		sourceDistances = append(sourceDistances, SourceDistance{
			Source:   source,
			Distance: distance,
		})
	}

	sort.Slice(sourceDistances, func(i, j int) bool {
		return sourceDistances[i].Distance < sourceDistances[j].Distance
	})

	return sourceDistances
}

func GetNearestSourcesSorted[T CoordinatesGetter](sources []T, latitude, longitude float64, maxCount int) []T {
	if len(sources) == 0 || maxCount <= 0 {
		return []T{}
	}

	sourceDistances := CalculateAndSortSourceDistances(sources, latitude, longitude)

	limit := maxCount
	if len(sourceDistances) < limit {
		limit = len(sourceDistances)
	}

	sortedSources := make([]T, limit)
	for i := 0; i < limit; i++ {
		sortedSources[i] = sourceDistances[i].Source.(T)
	}

	return sortedSources
}

func FilterSourcesByDistance[T CoordinatesGetter](sources []T, targetLat, targetLng float64) []T {
	var filtered []T
	for _, source := range sources {
		sourceLat, sourceLng := source.GetCoordinates()
		distance := HaversineDistance(targetLat, targetLng, sourceLat, sourceLng)
		if distance <= MaxDistanceKM {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

type DistanceResult struct {
	Distance float64
	Valid    bool
}

func CalculateDistanceToLocation(sourceLat, sourceLng float64, targetLocation LocationMatch) DistanceResult {
	if targetLocation.Latitude == 0 && targetLocation.Longitude == 0 {
		return DistanceResult{Distance: 0, Valid: false}
	}

	distance := HaversineDistance(sourceLat, sourceLng, targetLocation.Latitude, targetLocation.Longitude)
	return DistanceResult{Distance: distance, Valid: true}
}

func GetLocations(ctx context.Context, logger *slog.Logger, serviceabilityClient ServiceabilityClient) []LocationMatch {
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		logger.Error("Error loading program data", slog.String("error", err.Error()))
		metrics.DoublezeroExchangeFetchTotal.WithLabelValues("error").Inc()
		return []LocationMatch{}
	}

	if len(data.Exchanges) == 0 {
		logger.Warn("No exchanges found on-chain")
		metrics.DoublezeroExchangeFetchTotal.WithLabelValues("empty").Inc()
		return []LocationMatch{}
	}

	var locationMatches []LocationMatch
	for _, exc := range data.Exchanges {
		if exc.Status == serviceability.ExchangeStatusActivated {
			locationMatches = append(locationMatches, LocationMatch{
				LocationCode: exc.Code,
				Latitude:     exc.Lat,
				Longitude:    exc.Lng,
			})
		}
	}

	logger.Info("Loaded exchanges from blockchain",
		slog.Int("total_exchanges", len(data.Exchanges)),
		slog.Int("activated_exchanges", len(locationMatches)))

	metrics.DoublezeroExchanges.Set(float64(len(locationMatches)))

	return locationMatches
}

func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0

	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180

	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

func LoadLocationsFromJSON(logger *slog.Logger, filename string) ([]JSONLocation, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %w", err)
	}
	defer file.Close()

	var locations []JSONLocation
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&locations); err != nil {
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}

	if len(locations) == 0 {
		return nil, ErrLocationNotFound.WithContext("filename", filename).
			WithContext("reason", "JSON file contains empty array")
	}

	// Validate locations
	var validLocations []JSONLocation
	for i, loc := range locations {
		if loc.Code == "" {
			logger.Warn("Skipping location - missing code",
				slog.Int("index", i),
				slog.String("name", loc.Name))
			continue
		}
		if loc.Name == "" {
			logger.Warn("Skipping location - missing name",
				slog.Int("index", i),
				slog.String("code", loc.Code))
			continue
		}
		if loc.Latitude == 0 && loc.Longitude == 0 {
			logger.Warn("Skipping location - invalid coordinates",
				slog.Int("index", i),
				slog.String("code", loc.Code),
				slog.String("name", loc.Name))
			continue
		}
		validLocations = append(validLocations, loc)
	}

	if len(validLocations) == 0 {
		return nil, ErrLocationNotFound.WithContext("filename", filename).
			WithContext("total_locations", len(locations)).
			WithContext("reason", "no valid locations found")
	}

	logger.Info("Loaded locations from JSON file",
		slog.Int("location_count", len(validLocations)),
		slog.String("filename", filename))
	return validLocations, nil
}
