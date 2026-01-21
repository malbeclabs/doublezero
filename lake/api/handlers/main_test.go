package handlers_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
)

var testDB *apitesting.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	log := slog.Default()

	var err error
	testDB, err = apitesting.NewDB(ctx, log, nil)
	if err != nil {
		slog.Error("failed to start PostgreSQL container", "error", err)
		os.Exit(1)
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}
