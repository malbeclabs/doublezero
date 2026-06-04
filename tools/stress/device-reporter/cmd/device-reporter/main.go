// device-reporter consumes one or more stress-test run directories and
// emits insights — markdown summaries for paste-into-PR / standups, and
// CSV exports for spreadsheet / Python analysis.
//
// Subcommands:
//
//	device-reporter summary <run-dir>           # markdown summary to stdout
//	device-reporter compare <run-dir-a> <run-dir-b>  # side-by-side
//	device-reporter export  <run-dir> --metric commit-latency [--format csv]
//
// All output is stdout; pipe or redirect at will.
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
	case "compare":
		return cmdCompare(os.Args[2:])
	case "export":
		return cmdExport(os.Args[2:])
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
  device-reporter compare <run-dir-a> <run-dir-b>
  device-reporter export  <run-dir> --metric <commit-latency|runlog> [--format csv]
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

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("compare takes exactly two <run-dir-a> <run-dir-b>")
	}
	a, err := parser.LoadRun(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("load A: %w", err)
	}
	b, err := parser.LoadRun(fs.Arg(1))
	if err != nil {
		return fmt.Errorf("load B: %w", err)
	}
	c := analyze.BuildComparison(a, b)
	format.Comparison(os.Stdout, c)
	return nil
}

func cmdExport(args []string) error {
	// Accept the run-dir as the first positional arg followed by flags
	// (so `device-reporter export <dir> --metric ...` reads naturally),
	// then parse the remaining flags. Go's flag package requires flags
	// to precede positionals by default, so we peel the positional first.
	if len(args) < 1 || args[0] == "-h" || args[0] == "--help" {
		return fmt.Errorf("export takes <run-dir> followed by flags (see `device-reporter help`)")
	}
	runDir := args[0]
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	metric := fs.String("metric", "commit-latency", "metric to export: commit-latency | runlog")
	formatFlag := fs.String("format", "csv", "output format (csv only for now)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *formatFlag != "csv" {
		return fmt.Errorf("unsupported --format %q (only csv)", *formatFlag)
	}
	run, err := parser.LoadRun(runDir)
	if err != nil {
		return err
	}
	return format.ExportCSV(os.Stdout, run, format.ExportMetric(*metric))
}
