# End-to-End Testing

This directory contains our end-to-end tests which exercise the smartcontract, client, activator, and controller within a single docker container. A local validator (solana-test-validator) is used to apply the smartcontract and populate data onchain.

If you are running these for the first time, an image containing the solana CLI must be built. Unfortunately, Anza doesn't provide a package for linux-based arm64 platforms so if you are running these on apple silicon, the tools need to be compiled from source.

To build the solana CLI and test images, run the following in the `e2e` directory. Your platform arch should be autodetected and build the correct images:
```
make all
```

To run the tests, run:
```
make test
```

Once the solana image is built, if you are iterating on component code or troubleshooting tests, you only need to rebuild the test image:
```
make build
```

### Test Harness Details
- The logic and control flow of the end-to-end test setup are contained in `start_e2e.sh`.
- The actual tests are written in go in `e2e_test.go`, compiled into the test image, and executed within `start_e2e.sh`.
- Several of the tests rely on golden files (read: expected output) compared against `doublezero` command line output or controller configuration. These are stored in the `fixtures` directory.
- The test harness needs some work to eliminate some of the timing elements in the test script. There are several pauses within the test, waiting for the system to converge and are solely based on sleep statements.