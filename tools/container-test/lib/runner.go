package containertest

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	containerBuildImagePrefix = "container-test-runner"
)

type RunnerConfig struct {
	TestDir       string   `yaml:"test-dir"`
	Dockerfile    string   `yaml:"dockerfile"`
	DockerRunArgs []string `yaml:"docker-run-args"`

	Verbosity   int    `yaml:"verbosity"`
	NoFastFail  bool   `yaml:"no-fast-fail"`
	NoParallel  bool   `yaml:"no-parallel"`
	Parallelism int    `yaml:"parallelism"`
	TestPattern string `yaml:"test-pattern"`
}

type Runner struct {
	config RunnerConfig

	containerBuildImage string

	mu              sync.Mutex
	failedTests     []string
	passedTests     []string
	incompleteTests []string
	testTimings     map[string]time.Duration
	testsToRun      []string
}

func NewRunner(config RunnerConfig) (*Runner, error) {

	// Check required options.
	if config.Dockerfile == "" {
		return nil, fmt.Errorf("dockerfile is required")
	}

	// Set option defaults.
	if config.TestDir == "" {
		config.TestDir = "."
	}

	return &Runner{
		config: config,
	}, nil
}

func (r *Runner) Setup() error {
	// Initialize the container build image.
	r.containerBuildImage = fmt.Sprintf("%s-%s:dev", containerBuildImagePrefix, randomShortID())

	// Build the docker image.
	err := r.buildDockerImage()
	if err != nil {
		return err
	}

	// Get tests to run.
	r.testsToRun, err = r.getTestsToRun()
	if err != nil {
		return err
	}

	if r.config.Verbosity > 0 {
		fmt.Printf("--- INFO: Running with verbosity %d\n", r.config.Verbosity)
	}

	return nil
}

func (r *Runner) Cleanup() {}

func (r *Runner) buildDockerImage() error {
	// Find the first go.mod file in any parent directory.
	goModPath, err := findGoMod(r.config.TestDir)
	if err != nil {
		return fmt.Errorf("failed to find go.mod: %v", err)
	}
	goModDir := filepath.Dir(goModPath)
	if r.config.Verbosity > 2 {
		fmt.Printf("--- DEBUG: go.mod directory: %s\n", goModDir)
	}

	// Print current working directory.
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %v", err)
	}
	if r.config.Verbosity > 2 {
		fmt.Printf("--- DEBUG: Current working directory: %s\n", wd)
	}

	// Build the docker image.
	fmt.Printf("--- INFO: Building docker image %s (this may take a while)...\n", r.containerBuildImage)
	start := time.Now()
	buildCmd := exec.Command("docker", "build",
		"-t", r.containerBuildImage,
		"-f", r.config.Dockerfile,
		goModDir)
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	buildCmd.Dir = goModDir
	if r.config.Verbosity > 1 {
		fmt.Printf("--- DEBUG: Running: %s\n", strings.Join(buildCmd.Args, " "))
	}
	var output []byte
	if r.config.Verbosity > 0 {
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		err = buildCmd.Run()
	} else {
		output, err = buildCmd.CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("failed to build docker image\n%s", output)
	}
	fmt.Printf("--- OK: docker build (%.2fs)\n", time.Since(start).Seconds())
	return nil
}

func (r *Runner) getTestsToRun() ([]string, error) {
	var tests []string
	fset := token.NewFileSet()

	// Split pattern by slashes if present.
	// Match behaviour in https://pkg.go.dev/cmd/go/internal/test
	var patterns []*regexp.Regexp
	if r.config.TestPattern != "" {
		for _, p := range strings.Split(r.config.TestPattern, "/") {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("invalid test pattern: %v", err)
			}
			patterns = append(patterns, re)
		}
	}

	err := filepath.Walk(r.config.TestDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, "_test.go") {
			// Parse the file for test functions and build constraints.
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return fmt.Errorf("failed to parse %s: %v", path, err)
			}

			for _, decl := range f.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if strings.HasPrefix(funcDecl.Name.Name, "Test") {
					// If no pattern, run all tests
					if len(patterns) == 0 {
						tests = append(tests, funcDecl.Name.Name)
						continue
					}

					// Check if test name matches all parts of the pattern
					matches := true
					for _, re := range patterns {
						if !re.MatchString(funcDecl.Name.Name) {
							matches = false
							break
						}
					}
					if matches {
						tests = append(tests, funcDecl.Name.Name)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find tests: %v", err)
	}
	return tests, nil
}

func (r *Runner) RunTests() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	r.testTimings = make(map[string]time.Duration)

	suiteStart := time.Now()
	switch len(r.testsToRun) {
	case 1:
		fmt.Printf("--- INFO: Running 1 test...\n")
	case 0:
		fmt.Printf("--- INFO: No tests to run.\n")
	default:
		fmt.Printf("--- INFO: Running %d tests %s...\n", len(r.testsToRun), map[bool]string{true: "sequentially", false: fmt.Sprintf("in parallel (max %d)", r.config.Parallelism)}[r.config.NoParallel])
	}

	sem := make(chan struct{}, r.config.Parallelism)

	for _, test := range r.testsToRun {
		if r.config.NoParallel {
			r.runTest(ctx, test, cancel)
		} else {
			wg.Add(1)
			go func(test string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				r.runTest(ctx, test, cancel)
			}(test)
		}
	}

	if !r.config.NoParallel {
		wg.Wait()
	}
	suiteDuration := time.Since(suiteStart)

	r.printSummary(suiteDuration)

	if len(r.failedTests) > 0 {
		return fmt.Errorf("tests failed")
	}
	return nil
}

func (r *Runner) runTest(ctx context.Context, test string, cancel context.CancelFunc) {
	fmt.Printf("=== RUN: %s\n", test)
	start := time.Now()

	args := []string{"run", "--rm", "--tty",
		"--name", sanitizeContainerName(test)}
	if len(r.config.DockerRunArgs) > 0 {
		for _, arg := range r.config.DockerRunArgs {
			args = append(args, strings.Fields(arg)...)
		}
	}
	args = append(args, r.containerBuildImage, "-test.run", fmt.Sprintf("^%s$", test))
	if r.config.Verbosity > 0 {
		args = append(args, "-test.v")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if r.config.Verbosity > 1 {
		fmt.Printf("--- DEBUG: Running: %s\n", strings.Join(cmd.Args, " "))
	}

	var output bytes.Buffer
	if r.config.Verbosity > 0 {
		cmd.Stdout = io.MultiWriter(os.Stdout, &output)
		cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
	}

	if err := cmd.Run(); err != nil {
		r.mu.Lock()
		if len(r.failedTests) == 0 {
			if !r.config.NoFastFail {
				cancel()
				for _, t := range r.testsToRun {
					t = strings.TrimSpace(t)
					if t == "" || t == test {
						continue
					}
					ran := false
					for _, pt := range r.passedTests {
						if pt == t {
							ran = true
							break
						}
					}
					for _, ft := range r.failedTests {
						if ft == t {
							ran = true
							break
						}
					}
					if !ran {
						r.incompleteTests = append(r.incompleteTests, t)
					}
				}
			}
		}
		r.failedTests = append(r.failedTests, test)
		r.testTimings[test] = time.Since(start)
		r.mu.Unlock()
		if test == r.failedTests[0] {
			if r.config.Verbosity > 0 {
				fmt.Printf("--- FAIL: %s (%.2fs)\n", test, r.testTimings[test].Seconds())
			} else {
				fmt.Printf("--- FAIL: %s (%.2fs)\n%s", test, r.testTimings[test].Seconds(), output.String())
			}
		}
	} else {
		r.mu.Lock()
		r.passedTests = append(r.passedTests, test)
		r.testTimings[test] = time.Since(start)
		r.mu.Unlock()
		fmt.Printf("--- PASS: %s (%.2fs)\n", test, r.testTimings[test].Seconds())
	}
}

func (r *Runner) printSummary(suiteDuration time.Duration) {
	fmt.Println()
	if len(r.failedTests) == 0 {
		fmt.Printf("=== SUMMARY: PASS (%.2fs)\n", suiteDuration.Seconds())
		for _, test := range r.passedTests {
			fmt.Printf("PASS: %s (%.2fs)\n", test, r.testTimings[test].Seconds())
		}
	} else {
		fmt.Printf("=== SUMMARY: FAIL (%.2fs)\n", suiteDuration.Seconds())
		for _, test := range r.passedTests {
			fmt.Printf("PASS: %s (%.2fs)\n", test, r.testTimings[test].Seconds())
		}
		if !r.config.NoFastFail {
			for _, test := range r.failedTests {
				fmt.Printf("FAIL: %s (%.2fs)\n", test, r.testTimings[test].Seconds())
			}
		} else {
			fmt.Printf("FAIL: %s (%.2fs)\n", r.failedTests[0], r.testTimings[r.failedTests[0]].Seconds())
			for _, test := range r.incompleteTests {
				fmt.Printf("STOP: %s\n", test)
			}
		}
	}
}

// sanitizeContainerName converts a test name to a valid Docker container name
func sanitizeContainerName(testName string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	name := reg.ReplaceAllString(testName, "-")
	if !regexp.MustCompile(`^[a-zA-Z0-9]`).MatchString(name) {
		name = "e2e-" + name
	}
	if len(name) > 20 {
		name = name[:20]
	}
	return fmt.Sprintf("e2e-%s-%s", name, randomShortID())
}

func randomShortID() string {
	return fmt.Sprintf("%04x", rand.Intn(65536))
}

func findGoMod(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	for {
		goModPath := filepath.Join(absDir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return goModPath, nil
		}
		parent := filepath.Dir(absDir)
		if parent == absDir {
			return "", fmt.Errorf("go.mod not found in %s or any parent directory", absDir)
		}
		absDir = parent
	}
}
