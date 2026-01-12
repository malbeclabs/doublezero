package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
)

var (
	// ClickHouseURL is the URL for connecting to ClickHouse HTTP interface
	ClickHouseURL string
	// ClickHouseDatabase is the database name to query
	ClickHouseDatabase string
)

// Load initializes configuration from environment variables
func Load() error {
	addr := os.Getenv("CLICKHOUSE_ADDR_HTTP")
	if addr == "" {
		addr = "localhost:8123"
	}

	database := os.Getenv("CLICKHOUSE_DATABASE")
	if database == "" {
		database = "default"
	}

	username := os.Getenv("CLICKHOUSE_USERNAME")
	password := os.Getenv("CLICKHOUSE_PASSWORD")

	log.Printf("Loading ClickHouse configuration: addr: %s, database: %s, username: %s", addr, database, username)

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
	ClickHouseDatabase = database

	return nil
}

// ClickHouseBaseURL returns the base URL with database parameter (for POST requests)
func ClickHouseBaseURL() string {
	u, err := url.Parse(ClickHouseURL)
	if err != nil {
		return ClickHouseURL
	}
	// Keep only the database parameter for POST requests
	q := url.Values{}
	q.Set("database", ClickHouseDatabase)
	u.RawQuery = q.Encode()
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
