// device-reporter consumes a stress-test run directory and emits a
// markdown summary suitable for paste-into-PR / standups.
//
// Usage:
//
//	device-reporter summary <run-dir>
//
// Output goes to stdout; pipe or redirect at will.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/analyze"
	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/format"
	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		return fmt.Errorf("missing subcommand")
	}
	switch os.Args[1] {
	case "summary":
		return cmdSummary(os.Args[2:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, `device-reporter — stress-run analysis

Usage:
  device-reporter summary <run-dir>
`)
}

func cmdSummary(args []string) error {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	// 1 GiB default targets production-class Arista DUTs; `=0` disables.
	memFloorMB := fs.Uint64("free-mem-floor-mb", 1024, "free-memory floor in MiB; sub-floor `show processes top once` samples are counted as violations (0 disables)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("summary takes exactly one <run-dir>")
	}
	run, err := parser.LoadRun(fs.Arg(0))
	if err != nil {
		return err
	}
	// EOS reports memInfo in kilobytes; convert MiB → KiB once here.
	s := analyze.BuildSummary(run, analyze.Options{MemFreeFloorKB: *memFloorMB * 1024})
	format.Summary(os.Stdout, s, run)
	return nil
}
