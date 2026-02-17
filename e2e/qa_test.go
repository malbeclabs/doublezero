//go:build qa

package e2e

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
)

var (
	hostsFlag       = flag.String("hosts", "", "comma separated list of hosts to run tests against")
	portFlag        = flag.String("port", "7009", "port to connect to on each host")
	envFlag         = flag.String("env", "", "environment to run in (devnet, testnet, mainnet-beta)")
	debug           = flag.Bool("debug", false, "enable debug logging")
	multiTunnelFlag = flag.Bool("multi-tunnel", false, "enable multi-tunnel mode (connect unicast + multicast simultaneously)")

	hostsArg      []string
	portArg       int
	envArg        string
	networkConfig *config.NetworkConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
	switch *envFlag {
	case "devnet", "testnet", "mainnet-beta", "localnet":
	case "":
		log.Fatal("The -env flag is required. Must be one of: devnet, testnet, mainnet-beta, localnet")
	default:
		log.Fatalf("Invalid value for -env flag: %q. Must be one of: devnet, testnet, mainnet-beta, localnet", *envFlag)
	}

	hostsArg = strings.Split(*hostsFlag, ",")
	var err error
	portArg, err = strconv.Atoi(*portFlag)
	if err != nil {
		log.Fatalf("Invalid value for -port flag: %q. Must be an integer", *portFlag)
	}
	envArg = *envFlag

	networkConfig, err = config.NetworkConfigForEnv(envArg)
	if err != nil {
		log.Fatalf("failed to get network config: %v", err)
	}

	os.Exit(m.Run())
}

type testWriter struct {
	t  *testing.T
	mu sync.Mutex
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Logf("%s", p)
	return len(p), nil
}

func newTestLogger(t *testing.T) *slog.Logger {
	w := &testWriter{t: t}
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(w, &tint.Options{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(formatRFC3339Millis(t))
			}
			if s, ok := a.Value.Any().(string); ok && s == "" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func formatRFC3339Millis(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ms := t.Nanosecond() / 1_000_000
	return fmt.Sprintf("%s.%03dZ", base, ms)
}
