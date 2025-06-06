# End-to-End Testing

This directory contains our end-to-end tests which exercise the smartcontract, client, activator, and controller within a single docker container. A local validator (solana-test-validator) is used to apply the smartcontract and populate data onchain.

Docker images are built containing the doublezero components, tests, and solana CLI. They will automatically build whenever you run the tests from the `e2e` directory:

```
make test
```

### Test Harness Details

![topology](./assets/topology.png)

- The topology setup is handled in `bootstrap.sh`
- The logic and control flow of the end-to-end test setup are contained in `start_e2e.sh`.
- The actual tests are written in go in `e2e_test.go`, compiled into the test image, and executed within `start_e2e.sh`.
- Several of the tests rely on golden files (read: expected output) compared against `doublezero` command line output or controller configuration. These are stored in the `fixtures` directory.
- The test harness needs some work to eliminate some of the timing elements in the test script. There are several pauses within the test, waiting for the system to converge and are solely based on sleep statements.
