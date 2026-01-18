package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4j is the global Neo4j driver
var Neo4j neo4j.DriverWithContext

// Neo4jDatabase is the configured database name
var Neo4jDatabase string

// LoadNeo4j initializes the Neo4j driver from environment variables
func LoadNeo4j() error {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}

	Neo4jDatabase = os.Getenv("NEO4J_DATABASE")
	if Neo4jDatabase == "" {
		Neo4jDatabase = "neo4j"
	}

	username := os.Getenv("NEO4J_USERNAME")
	if username == "" {
		username = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")

	log.Printf("Connecting to Neo4j: uri=%s, database=%s, username=%s", uri, Neo4jDatabase, username)

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	Neo4j = driver
	log.Printf("Connected to Neo4j successfully")

	return nil
}

// CloseNeo4j closes the Neo4j driver
func CloseNeo4j() error {
	if Neo4j != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return Neo4j.Close(ctx)
	}
	return nil
}

// Neo4jSession creates a new Neo4j session
func Neo4jSession(ctx context.Context) neo4j.SessionWithContext {
	return Neo4j.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: Neo4jDatabase,
	})
}
