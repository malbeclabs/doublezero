#!/bin/bash

cargo run -- --rpc-url "https://api.devnet.solana.com" location create --code LA --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
cargo run -- --rpc-url "https://api.devnet.solana.com" location create --code NY --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925

cargo run -- --rpc-url "https://api.devnet.solana.com" exchange create --code LA --name "Los Angeles" --lat 40.049641274076464 --lng -118.25939642499903
cargo run -- --rpc-url "https://api.devnet.solana.com" exchange create --code NY --name "New York" --lat 40.780297071772125 --lng -74.07203003496925


cargo run -- --rpc-url "https://api.devnet.solana.com" device create --code LA1 --contributor co01 --location AxpUt7qU4YXmDcQunXX5fQ156FnDkreQRfKMjaYS3o2o --exchange Hr9mgHEDhqZZWjaNTcZkvp3thbb88cLtJ5x3NQG4hKXJ --public-ip "1.0.0.1"
cargo run -- --rpc-url "https://api.devnet.solana.com" device create --code NY1 --contributor co01 --location E6tpvcwm8oopL6ckz8h5aUMjoPxUz7RHC6vEb8A36PKH --exchange 6pT37rRRxp8CY9cVch6pbFDMSBC9MRhHfecXWH3YEJoZ --public-ip "1.0.0.2"

cargo run -- --rpc-url "https://api.devnet.solana.com" Link create --contributor co01 --code "LA1-NY1" --side-a b26HEfQkk1DYawcpj9KYF5Wk6Bs6Q9rYWLbsxRoaFMH --side-z WnSXBCykWjuJDm8e1r5cen32JfoSNbKbxnW8bM5LKkr --bandwidth 100 --mtu 9000 --delay 15 --jitter 1



