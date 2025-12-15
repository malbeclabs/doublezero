package metrodb

import (
	"embed"
	"encoding/csv"
	"fmt"
	"strings"
)

//go:embed data/metros.csv
var metroCSV embed.FS

type key struct {
	city    string
	country string
}

type MetroDB struct {
	byKey map[key]string
}

func New() (*MetroDB, error) {
	f, err := metroCSV.Open("data/metros.csv")
	if err != nil {
		return nil, fmt.Errorf("failed to open embedded csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.ReuseRecord = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read csv: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("csv is empty")
	}

	byKey := make(map[key]string, len(records)-1)
	for i, rec := range records {
		if i == 0 {
			continue
		}
		if len(rec) < 3 {
			continue
		}
		city := strings.TrimSpace(rec[0])
		country := strings.ToUpper(strings.TrimSpace(rec[1]))
		metro := strings.TrimSpace(rec[2])
		if city == "" || country == "" || metro == "" {
			continue
		}
		byKey[key{city: city, country: country}] = metro
	}

	return &MetroDB{byKey: byKey}, nil
}

func (db *MetroDB) Lookup(city, countryCode string) (string, bool) {
	if db == nil {
		return "", false
	}
	k := key{
		city:    strings.TrimSpace(city),
		country: strings.ToUpper(strings.TrimSpace(countryCode)),
	}
	v, ok := db.byKey[k]
	return v, ok
}
