# End-to-End Testing

This directory contains our end-to-end tests which exercise the smartcontract, client, activator, and controller within a single docker container. A local validator (solana-test-validator) is used to apply the smartcontract and populate data onchain.

These tests depend on images exposed via our container registry. To access, you need to login with your github username and a personal access token with `read:packages` access. See [this github doc](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-with-a-personal-access-token-classic) for more details.

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
