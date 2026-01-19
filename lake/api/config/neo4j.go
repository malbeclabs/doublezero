package config

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

// Neo4jClient is the global read-only Neo4j client
var Neo4jClient neo4j.Client

// Neo4jDatabase is the configured database name
var Neo4jDatabase string

// LoadNeo4j initializes the Neo4j client from environment variables.
// The client is read-only to prevent accidental writes from the API layer.
func LoadNeo4j() error {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}

	Neo4jDatabase = os.Getenv("NEO4J_DATABASE")
	if Neo4jDatabase == "" {
		Neo4jDatabase = neo4j.DefaultDatabase
	}

	username := os.Getenv("NEO4J_USERNAME")
	if username == "" {
		username = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")

	log.Printf("Connecting to Neo4j (read-only): uri=%s, database=%s, username=%s", uri, Neo4jDatabase, username)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := neo4j.NewReadOnlyClient(ctx, slog.Default(), uri, Neo4jDatabase, username, password)
	if err != nil {
		return err
	}

	Neo4jClient = client
	log.Printf("Connected to Neo4j successfully (read-only)")

	return nil
}

// CloseNeo4j closes the Neo4j client
func CloseNeo4j() error {
	if Neo4jClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return Neo4jClient.Close(ctx)
	}
	return nil
}

// Neo4jSession creates a new Neo4j session
func Neo4jSession(ctx context.Context) neo4j.Session {
	session, _ := Neo4jClient.Session(ctx)
	return session
}
