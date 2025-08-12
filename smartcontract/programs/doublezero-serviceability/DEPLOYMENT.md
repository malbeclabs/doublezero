# Deployment Process for a Smart Contract on Solana to DZ Lager

This document describes the formal process for deploying a smart contract (program) to the DZ Lager sidechain, as used in the DoubleZero protocol. The process consists of three main stages: running unit tests, compiling the program for Solana's SBF target, and deploying the program to the DZ Lager network.

## 1. Prerequisites

- Ensure your Solana CLI is configured with the correct keypair and network settings for DZ Lager.

```bash
solana config set --url https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16 --ws wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig
```

- You must have the keypair file for the version of the smart contract you are deploying (e.g., GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah).
- You must also have the id.json keypair file for the authority account that is allowed to update the smart contract on-chain.
- You may need to airdrop SOL to your deployment account on DZ Lager for transaction fees.

## 2. Run Unit Tests

Before deploying, ensure the smart contract passes all unit tests. This step helps catch logic errors and ensures code quality.

```bash
cargo test -- --nocapture
```

- This command runs all Rust unit tests in the project.
- Ensure all tests pass before proceeding.
- The output should end with a line similar to:

  `test result: ok. 34 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.37s`

  This confirms that all tests have passed successfully.

## 3. Compile the Program for Solana (SBF)

Solana smart contracts must be compiled to the SBF (Solana BPF) target. Use the following command:

```bash
cargo build-sbf
```

- This command compiles the program to a `.so` (shared object) file suitable for deployment on Solana and DZ Lager.
- The output will be located in the `target/deploy/` directory (e.g., `target/deploy/doublezero_serviceability.so`).
- **Important:** You must ensure that the compiled file is named `doublezero_serviceability.so` and is present in the `./target/deploy` directory before proceeding to deployment. This file is required by the deployment command.

## 4. Deploy the Program to DZ Lager

With the program compiled, deploy it to the DZ Lager sidechain using the Solana CLI:

- RPC = https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16
- WS = wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig
- Keypair = ~/GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah.json
- Binary file = ./target/deploy/doublezero_serviceability.so

### Command

```bash
solana program deploy --program-id ~/GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah.json target/deploy/doublezero_serviceability.so
```

## 5 Next steps

- After deploying a new version of the smart contract, ensure that all clients update their SDK and CLI to the corresponding version to maintain compatibility.

---

For more information, see the [Solana CLI documentation](https://docs.solana.com/cli/deploy-a-program).
