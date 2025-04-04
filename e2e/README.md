# End-to-End Testing

This directory contains our end-to-end tests which exercise the smartcontract, client, activator, and controller within a single docker container. A local validator (solana-test-validator) is used to apply the smartcontract and populate data onchain.

If you are running these for the first time, an image containing the solana CLI must be built. Unfortunately, Anza doesn't provide a package for linux-based arm64 platforms so if you are running these on apple silicon, the tools need to be compiled from source.

These tests depend on images exposed via our container registry. To access, you need to login with your github username and a personal access token with `read:packages` access. See [this github doc](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-with-a-personal-access-token-classic) for more details.

To build the solana CLI and test images, run the following in the `e2e` directory. Your platform arch should be autodetected and build the correct images:
```
make all
```

To run the tests, run:
```
sudo make test
```

Once the solana image is built, if you are iterating on component code or troubleshooting tests, you only need to rebuild the test image:
```
make build
```

### Test Harness Details

![topology](./assets/topology.png)

- The topology setup is handled in `bootstrap.sh`
- The logic and control flow of the end-to-end test setup are contained in `start_e2e.sh`.
- The actual tests are written in go in `e2e_test.go`, compiled into the test image, and executed within `start_e2e.sh`.
- Several of the tests rely on golden files (read: expected output) compared against `doublezero` command line output or controller configuration. These are stored in the `fixtures` directory.
- The test harness needs some work to eliminate some of the timing elements in the test script. There are several pauses within the test, waiting for the system to converge and are solely based on sleep statements.