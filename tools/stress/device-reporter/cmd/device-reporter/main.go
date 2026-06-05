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
	s := analyze.BuildSummary(run)
	format.Summary(os.Stdout, s, run)
	return nil
}
