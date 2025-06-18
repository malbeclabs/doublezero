package twamplight_test

import (
	"flag"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/lmittmann/tint"
)

var (
	log *slog.Logger
)

// TestMain sets up the test environment with a global logger.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	log = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.RFC3339,
		AddSource:  true,
	}))

	os.Exit(m.Run())
}
