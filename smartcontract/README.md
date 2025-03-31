# Smart Contract on Solana

This repository contains a smart contract written for the Solana blockchain. The smart contract is the **core permissionless component** that defines and manages the **logical layer** of the **DoubleZero** network. It allows both contributors and users to interact seamlessly—contributors can create and configure services, while users can connect to access and use them.

By establishing accounts linked to each component, the smart contract **store** the necessary parameters, enabling client controllers and services to provision **network functions** and **network features** such as **deduplication, signature verification, and other**. This ensures a decentralized, automated, and efficient approach to network service management.

## Prerequisites

Before you begin, ensure you have met the following requirements:
- Rust installed on your machine. You can download it from [rust-lang.org](https://www.rust-lang.org/).
- Solana CLI installed. Follow the instructions [here](https://docs.solana.com/cli/install-solana-cli-tools) to install it.
- Node.js and npm installed. You can download them from [nodejs.org](https://nodejs.org/).

## Installation

1. Clone the repository:
    ```sh
    git clone https://github.com/yourusername/your-repo-name.git
    cd your-repo-name
    ```

2. Install the dependencies:
    ```sh
    npm install
    ```

3. Build the smart contract:
    ```sh
    npm run build
    ```

## Deployment

To deploy the smart contract to the Solana blockchain, follow these steps:

1. Set up your Solana CLI configuration:
    ```sh
    solana config set --url https://api.devnet.solana.com
    ```

2. Generate a new keypair to pay gas fees (or use an existing one):
    ```sh
    solana-keygen new --outfile /path/to/my-payer-keypair.json
    ```

3. Airdrop some SOL to your payer account if needed:
    ```sh
    solana airdrop 2 ~/my-keypair.json
    ```

4. Generate a new keypair to deploy the program (or use an existing one): 
    ```sh
    solana-keygen new --outfile /path/to/my-deployer-keypair.json
    ```

5. Build the smart contract:
    ```sh
    cargo build-sbf --manifest-path smartcontract/programs/dz-sla-program/Cargo.toml
    ```

6. Deploy the smart contract:
    Save the program ID displayed by this command for future reference.
    ```sh
    solana program deploy --fee-payer /path/to/my-payer-keypair.json -k /path/to/my-deployer-keypair.json ./smartcontract/programs/dz-sla-program/target/deploy/double_zero_sla_program.so
    ```

7. Initialize the smart contract using the program ID output by step 6:
    ```sh
    target/x86_64-unknown-linux-gnu/release/doublezero --program-id <Program ID> init 
    ```
GlobalConfig: GlobalState { account_type: GlobalState, account_index: 0, foundation_allowlist: [DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan], device_allowlist: [DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan], user_allowlist: [DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan] }
    ```

## Usage

To interact with the deployed smart contract, you can use the provided scripts or write your own. Here is an example of how to call a function from the smart contract:

```sh
solana program invoke --program-id <PROGRAM_ID> --keypair ~/my-keypair.json --data <DATA>
```

## Contributing

Contributions are always welcome! Please follow these steps:

1. Fork the repository.
2. Create a new branch (`git checkout -b feature-branch`).
3. Make your changes.
4. Commit your changes (`git commit -m 'Add some feature'`).
5. Push to the branch (`git push origin feature-branch`).
6. Open a pull request.
