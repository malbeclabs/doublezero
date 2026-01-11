package config

import (
	"fmt"
	"net/url"
	"os"
)

var (
	// ClickHouseURL is the URL for connecting to ClickHouse HTTP interface
	ClickHouseURL string
)

// Load initializes configuration from environment variables
func Load() error {
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		addr = "localhost:8123"
	}

	database := os.Getenv("CLICKHOUSE_DATABASE")
	if database == "" {
		database = "default"
	}

	username := os.Getenv("CLICKHOUSE_USERNAME")
	password := os.Getenv("CLICKHOUSE_PASSWORD")

	// Build the ClickHouse URL
	u := &url.URL{
		Scheme: "http",
		Host:   addr,
	}

	if username != "" {
		if password != "" {
			u.User = url.UserPassword(username, password)
		} else {
			u.User = url.User(username)
		}
	}

	q := u.Query()
	q.Set("database", database)
	u.RawQuery = q.Encode()

	ClickHouseURL = u.String()

	return nil
}

// ClickHouseBaseURL returns the base URL without query parameters (for POST requests)
func ClickHouseBaseURL() string {
	u, err := url.Parse(ClickHouseURL)
	if err != nil {
		return ClickHouseURL
	}
	u.RawQuery = ""
	return u.String()
}

// ClickHouseQueryURL returns a URL with the given query appended
func ClickHouseQueryURL(query string) string {
	u, err := url.Parse(ClickHouseURL)
	if err != nil {
		return fmt.Sprintf("%s&query=%s", ClickHouseURL, url.QueryEscape(query))
	}
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()
	return u.String()
}
