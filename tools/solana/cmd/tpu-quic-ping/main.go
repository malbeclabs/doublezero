package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
)

func main() {
	duration := flag.Duration("duration", tpuquic.DefaultDuration, "how long to keep the QUIC connection open")
	interval := flag.Duration("interval", tpuquic.DefaultInterval, "how often to print connection stats")
	timeout := flag.Duration("timeout", tpuquic.DefaultTimeout, "how long to wait for the connection to be established")
	srcAddr := flag.String("src-addr", tpuquic.DefaultSrc.String(), "source address to bind (optional)")
	quiet := flag.Bool("q", false, "quiet mode - only show errors")
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

		Quiet:    *quiet,
		Duration: *duration,
		Interval: *interval,
		Timeout:  *timeout,
		Src:      *srcAddr,
		Dst:      dstAddr,
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
