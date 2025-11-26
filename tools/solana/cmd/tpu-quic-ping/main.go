package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	flag "github.com/spf13/pflag"
)

func main() {
	count := flag.IntP("count", "c", tpuquic.DefaultCount, "how many intervals to ping for (optional)")
	interval := flag.DurationP("interval", "i", tpuquic.DefaultInterval, "how often to print connection stats (optional)")
	timeout := flag.DurationP("timeout", "t", tpuquic.DefaultTimeout, "how long to wait for the connection to be established (optional)")
	srcAddr := flag.StringP("src", "S", tpuquic.DefaultSrc.String(), "source address to bind to (optional)")
	iface := flag.StringP("interface", "I", "", "interface to bind to (optional)")
	quiet := flag.BoolP("quiet", "q", false, "quiet mode - only show errors")

	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Printf("Usage: %s [flags] <dst-ip>:<dst-port>\n", os.Args[0])
		flag.PrintDefaults()
		return
	}
	dstAddr := flag.Arg(0)

	log := newLogger(*quiet)
	cfg := tpuquic.PingConfig{
		Logger: log,

		Quiet:     *quiet,
		Count:     *count,
		Interval:  *interval,
		Timeout:   *timeout,
		Src:       *srcAddr,
		Interface: *iface,
		Dst:       dstAddr,
	}

	result, err := tpuquic.Ping(cfg)
	if err != nil {
		if !*quiet {
			log.Error("Failed to ping", "error", err)
		}
		os.Exit(1)
	}
	if result.Error != nil {
		if !*quiet {
			log.Error("Failed to ping", "error", result.Error)
		}
		os.Exit(1)
	}
}

func newLogger(quiet bool) *slog.Logger {
	var writer io.Writer
	if quiet {
		writer = io.Discard
	} else {
		writer = os.Stdout
	}
	return slog.New(tint.NewHandler(writer, &tint.Options{
		Level: slog.LevelInfo,
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
