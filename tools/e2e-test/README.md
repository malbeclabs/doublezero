# E2E test runner

A tool for running Go tests in isolated containers with parallel execution.

## Usage

The tool will automatically find `e2e.yaml` config files in the current directory and its subdirectories, and execute containerized tests in parallel for each, based on the configuration in the package.

See the [client/doublezerod/internal/runtime](../../client/doublezerod/internal/runtime) tests as an example:
- [e2e.yaml](../../client/doublezerod/internal/runtime/e2e.yaml)
- [e2e.dockerfile](../../client/doublezerod/internal/runtime/e2e.dockerfile)
- [run_test.go](../../client/doublezerod/internal/runtime/run_test.go)


These are defined as regular Go tests, so if you don't want them to run with `go test` outside of containers you should include something like `//go:build e2e` at the top of your test files.

Run tests across all packages:

```bash
go run tools/e2e-test/main.go
```

Run tests based on a given test pattern, similar to `go test`:

```bash
go run tools/e2e-test/main.go TestEndToEnd_IBRL_WithAllocatedIP
```

Run tests from a package directory:

```bash
cd client/doublezerod/internal/runtime/
go run github.com/malbeclabs/doublezero/tools/e2e-test
```

## CLI Options

```
  -f string
        Config filename to search for recursively (default: e2e.yaml) (default "e2e.yaml")
  -help
        Show help
  -no-fast-fail
        Run all tests even if one fails (default: false)
  -no-parallel
        Run tests sequentially instead of in parallel (default: false)
  -p int
        Number of tests to run in parallel (default: number of CPUs)
  -run string
        Run only tests matching the pattern (default: all tests)
  -verbose int
        Verbosity level (default: 0)
```

## Example

```
$ go run tools/e2e-test/main.go

=== Running tests from client/doublezerod/internal/runtime/e2e.yaml ===
--- INFO: Building docker image e2e-test-runner-87bc:dev (this may take a while)...
--- OK: docker build (2.70s)
--- INFO: Running 6 tests in parallel (max 10)...
=== RUN: TestServiceCoexistence
=== RUN: TestEndToEnd_EdgeFiltering
=== RUN: TestMulticastPublisher
=== RUN: TestEndToEnd_IBRL_Basic
=== RUN: TestEndToEnd_IBRL_WithAllocatedIP
=== RUN: TestMulticastSubscriber
--- PASS: TestEndToEnd_EdgeFiltering (20.35s)
--- PASS: TestMulticastSubscriber (20.56s)
--- PASS: TestMulticastPublisher (20.59s)
--- PASS: TestEndToEnd_IBRL_WithAllocatedIP (20.61s)
--- PASS: TestEndToEnd_IBRL_Basic (25.67s)
--- PASS: TestServiceCoexistence (30.62s)

=== SUMMARY: PASS (30.62s)
PASS: TestEndToEnd_EdgeFiltering (20.35s)
PASS: TestMulticastSubscriber (20.56s)
PASS: TestMulticastPublisher (20.59s)
PASS: TestEndToEnd_IBRL_WithAllocatedIP (20.61s)
PASS: TestEndToEnd_IBRL_Basic (25.67s)
PASS: TestServiceCoexistence (30.62s)
```
