package containertest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	containertest "github.com/malbeclabs/doublezero/tools/container-test/lib"
)

const (
	dockerfile = `FROM golang:1.24.3-alpine AS builder
WORKDIR /work
COPY . .
RUN go test -c -o /bin/example.test -tags container_tests

FROM ubuntu:22.04
RUN apt-get update && \
    apt-get install -y ca-certificates
WORKDIR /work
COPY --from=builder /bin/example.test /bin/
ENTRYPOINT ["/bin/example.test"]
CMD ["-test.v"]
`

	allPassingTestFile = `
//go:build container_tests

package example

import (
	"testing"
)

func TestExample_ExpectedToPass1(t *testing.T) {
	t.Log("Hello, world 1!")
}

func TestExample_ExpectedToPass2(t *testing.T) {
	t.Log("Hello, world 2!")
}
`

	someFailingTestFile = `
//go:build container_tests

package example

import (
	"testing"
)

func TestExample_ExpectedToPass(t *testing.T) {
	t.Log("Hello, world 1!")
}

func TestExample_ExpectedToFail(t *testing.T) {
	t.Log("Hello, world 2!")
	t.Fail()
}
`
)

func TestContainerTest_Runner_AllPassingTests(t *testing.T) {
	// Create temporary directory for the test project.
	tmpDir, err := os.MkdirTemp("", "container-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Add a Dockerfile to the temporary directory.
	dockerfilePath := filepath.Join(tmpDir, "test.dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Add a simple test file to the temporary directory.
	testFilePath := filepath.Join(tmpDir, "example_test.go")
	if err := os.WriteFile(testFilePath, []byte(allPassingTestFile), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Add a go.mod file to the temporary directory.
	goModFilePath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModFilePath, []byte("module example\n\ngo 1.24.3\n"), 0644); err != nil {
		t.Fatalf("failed to write go.mod file: %v", err)
	}

	// Create and setup a test runner.
	verbosity := -1
	if os.Getenv("DEBUG") != "" {
		verbosity = 1
	}
	runner, err := containertest.NewRunner(containertest.RunnerConfig{
		TestDir:     tmpDir,
		Dockerfile:  dockerfilePath,
		Parallelism: 1,
		Verbosity:   verbosity,
	})
	if err != nil {
		t.Fatalf("failed to create test runner: %v", err)
	}
	if err := runner.Setup(); err != nil {
		t.Fatalf("failed to setup test runner: %v", err)
	}
	defer runner.Cleanup()

	// Execute the test runner (run the tests).
	if err := runner.RunTests(); err != nil {
		t.Fatalf("failed to run tests: %v", err)
	}
}

func TestContainerTest_Runner_SomeFailingTests(t *testing.T) {
	// Create temporary directory for the test project.
	tmpDir, err := os.MkdirTemp("", "container-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Add a Dockerfile to the temporary directory.
	dockerfilePath := filepath.Join(tmpDir, "test.dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Add a simple test file to the temporary directory.
	testFilePath := filepath.Join(tmpDir, "example_test.go")
	if err := os.WriteFile(testFilePath, []byte(someFailingTestFile), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Add a go.mod file to the temporary directory.
	goModFilePath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModFilePath, []byte("module example\n\ngo 1.24.3\n"), 0644); err != nil {
		t.Fatalf("failed to write go.mod file: %v", err)
	}

	// Create and setup a test runner.
	verbosity := -1
	if os.Getenv("DEBUG") != "" {
		verbosity = 1
	}
	runner, err := containertest.NewRunner(containertest.RunnerConfig{
		TestDir:     tmpDir,
		Dockerfile:  dockerfilePath,
		Parallelism: 1,
		Verbosity:   verbosity,
	})
	if err != nil {
		t.Fatalf("failed to create test runner: %v", err)
	}
	if err := runner.Setup(); err != nil {
		t.Fatalf("failed to setup test runner: %v", err)
	}
	defer runner.Cleanup()

	// Execute the test runner (run the tests).
	if err := runner.RunTests(); err == nil || !strings.Contains(err.Error(), "tests failed") {
		t.Fatalf("expected test to fail with error containing 'tests failed' but got: %v", err)
	}
}
