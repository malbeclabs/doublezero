package handlers_test

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
)

var (
	testPgDB    *apitesting.DB
	testChDB    *apitesting.ClickHouseDB
	testNeo4jDB *apitesting.Neo4jDB
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	log := slog.Default()

	var wg sync.WaitGroup
	var pgErr, chErr, neo4jErr error

	// Start all containers in parallel
	wg.Add(3)

	go func() {
		defer wg.Done()
		testPgDB, pgErr = apitesting.NewDB(ctx, log, nil)
	}()

	go func() {
		defer wg.Done()
		testChDB, chErr = apitesting.NewClickHouseDB(ctx, log, nil)
	}()

	go func() {
		defer wg.Done()
		testNeo4jDB, neo4jErr = apitesting.NewNeo4jDB(ctx, log, nil)
	}()

	wg.Wait()

	// Check for errors
	if pgErr != nil {
		slog.Error("failed to start PostgreSQL container", "error", pgErr)
		os.Exit(1)
	}
	if chErr != nil {
		slog.Error("failed to start ClickHouse container", "error", chErr)
		os.Exit(1)
	}
	if neo4jErr != nil {
		slog.Error("failed to start Neo4j container", "error", neo4jErr)
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup all containers
	if testPgDB != nil {
		testPgDB.Close()
	}
	if testChDB != nil {
		testChDB.Close()
	}
	if testNeo4jDB != nil {
		testNeo4jDB.Close()
	}

	os.Exit(code)
}
