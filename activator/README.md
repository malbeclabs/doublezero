# DoubleZero Activator

The DoubleZero activator service is responsible for smartcontract IP address allocation, tunnel allocation and activation.

## Build/Install Instructions

To build the activator service, install rust as a dependency and run `make build`:
```
$ curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

$ make build

$ ./target/release/doublezero-activator -h
Double Zero 

Usage: doublezero-activator [OPTIONS]

Options:
      --rpc <RPC>                
      --ws <WS>                  
      --program-id <PROGRAM_ID>  
      --keypair <KEYPAIR>        
  -h, --help                     Print help
  -V, --version                  Print version
```