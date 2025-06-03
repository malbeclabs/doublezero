package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	e2etest "github.com/malbeclabs/doublezero/tools/e2e-test/lib"
	"gopkg.in/yaml.v3"
)

var (
	defaultParallelism = runtime.NumCPU()
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("--- ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var configFile string
	var verbosity int
	var noFastFail bool
	var noParallel bool
	var parallelism int
	var testPattern string

	config := e2etest.RunnerConfig{}

	preprocessArgsForVerbosity()

	flag.StringVar(&configFile, "f", "e2e.yaml", "Config filename to search for recursively (default: e2e.yaml)")
	flag.IntVar(&verbosity, "verbose", 0, "Verbosity level (default: 0)")
	flag.BoolVar(&noFastFail, "no-fast-fail", false, "Run all tests even if one fails (default: false)")
	flag.BoolVar(&noParallel, "no-parallel", false, "Run tests sequentially instead of in parallel (default: false)")
	flag.IntVar(&parallelism, "parallelism", defaultParallelism, "Number of tests to run in parallel (default: number of CPUs)")
	flag.IntVar(&parallelism, "p", defaultParallelism, "Number of tests to run in parallel (default: number of CPUs)")
	flag.StringVar(&testPattern, "run", "", "Run only tests matching the pattern (default: all tests)")
	help := flag.Bool("help", false, "Show help")

	flag.Parse()

	// Show help if requested
	if *help {
		flag.Usage()
		return nil
	}

	// Set the flags-only config values
	config.Verbosity = verbosity
	config.NoFastFail = noFastFail
	config.NoParallel = noParallel
	config.Parallelism = parallelism
	config.TestPattern = testPattern

	// Find all e2e.yaml files recursively
	if verbosity > 2 {
		fmt.Printf("--- INFO: Finding e2e.yaml files recursively\n")
	}

	walker := e2etest.NewFileWalker(configFile, verbosity, ".")
	configFiles, err := walker.FindConfigFiles()
	if err != nil {
		return fmt.Errorf("failed to find e2e config files: %v", err)
	}

	if len(configFiles) == 0 {
		return fmt.Errorf("no e2e.yaml files found")
	}

	// Run each config file
	for _, configFile := range configFiles {
		fmt.Printf("\n=== Running tests from %s ===\n", configFile)

		// Get the absolute path of the config file
		absConfigFile, err := filepath.Abs(configFile)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of config file: %v", err)
		}

		// Read the config file
		data, err := os.ReadFile(absConfigFile)
		if err != nil {
			return fmt.Errorf("failed to read config file: %v", err)
		}

		// Parse the config file
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse config file: %v", err)
		}

		configDir := filepath.Dir(absConfigFile)
		if config.Dockerfile != "" {
			configFileDir := filepath.Dir(absConfigFile)
			config.Dockerfile = filepath.Join(configFileDir, config.Dockerfile)
		}

		// The test dir for this run is the directory of the config file.
		config.TestDir = configDir

		runner, err := e2etest.NewRunner(config)
		if err != nil {
			return err
		}
		if err := runner.Setup(); err != nil {
			return err
		}
		defer runner.Cleanup()

		if err := runner.RunTests(); err != nil {
			return err
		}
	}

	return nil
}

func preprocessArgsForVerbosity() {
	newArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		switch {
		case strings.HasPrefix(arg, "-vvv") && len(arg) == 4:
			newArgs = append(newArgs, "--verbose=3")
		case strings.HasPrefix(arg, "-vv") && len(arg) == 3:
			newArgs = append(newArgs, "--verbose=2")
		case arg == "-v":
			newArgs = append(newArgs, "--verbose=1")
		default:
			newArgs = append(newArgs, arg)
		}
	}
	os.Args = newArgs
}
