package wheresitup

import (
	"flag"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/lmittmann/tint"
)

var (
	logger *slog.Logger
)

func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	if verbose {
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.RFC3339,
			AddSource:  true,
		}))
	} else {
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level: slog.LevelWarn,
		}))
	}

	os.Exit(m.Run())
}
