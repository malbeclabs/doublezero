# DoubleZero Rust SDK

This SDK provides a Rust interface for interacting with the DoubleZero Solana smart contract. It enables developers to programmatically create, update, and manage all core DoubleZero entities (Locations, Exchanges, Devices, Tunnels, Users) and to invoke all supported instructions in a type-safe and ergonomic way.

## Features
- Type-safe Rust bindings for all DoubleZero smart contract instructions
- Account structure definitions matching on-chain state
- Helper functions for building and sending Solana transactions
- Support for permissionless, multi-contributor workflows

## Installation
Add the SDK to your Rust project by including it in your `Cargo.toml`:

```toml
[dependencies]
doublezero-sdk = { path = "../../sdk/rs" }
```

> Adjust the path as needed for your project structure.

## Usage

### 1. Import the SDK
```rust
use doublezero_sdk::*;
```

## Example: Using a Command

To use a command, create the corresponding struct, set its arguments, and call `execute(client)` with your DoubleZero client instance. For example, to create a tunnel:

```rust
use doublezero_sdk::commands::tunnel::CreateTunnelCommand;
use doublezero_sdk::DoubleZeroClient;
use solana_sdk::pubkey::Pubkey;
use doublezero_sla_program::state::tunnel::TunnelTunnelType;

// Prepare your arguments
let result = CreateTunnelCommand {
    code: "TUNNEL-001".to_string(),
    side_a_pk: [...],
    side_z_pk: [...],
    bandwidth: 1_000_000,
    mtu: 1500,
    delay_ns: 0,
    jitter_ns: 0,
}.execute(&client);

match result {
    Ok((signature, tunnel_pubkey)) => println!("Tunnel created: {} {}", signature, tunnel_pubkey),
    Err(e) => eprintln!("Error: {}", e),
}
```

Replace the arguments and client as needed for your use case. This pattern applies to all commands in the SDK.


## Main Structures Diagram

Below is a class diagram representing the main on-chain state structures managed by the DoubleZero protocol. Each structure's primary fields and relationships are shown, with PDA (Program Derived Address) public keys marked as primary keys.

```mermaid
classDiagram
    class Location {
        +Pubkey pda_pubkey
        String code
        String name
        String country
        f64 lat
        f64 lng
        u32 loc_id
    }
    class Exchange {
        +Pubkey pda_pubkey
        String code
        String name
        f64 lat
        f64 lng
        u32 loc_id
    }
    class Device {
        +Pubkey pda_pubkey
        String code
        DeviceType device_type
        Pubkey location_pk
        Pubkey exchange_pk
        IpV4 public_ip
        NetworkV4List dz_prefixes
    }
    class Tunnel {
        +Pubkey pda_pubkey
        String code
        Pubkey side_a_pk
        Pubkey side_z_pk
        TunnelTunnelType tunnel_type
        u64 bandwidth
        u32 mtu
        u64 delay_ns
        u64 jitter_ns
        u16 tunnel_id
        NetworkV4 tunnel_net
    }
    class User {
        +Pubkey pda_pubkey
        UserType user_type
        Pubkey device_pk
        UserCYOA cyoa_type
        IpV4 client_ip
        IpV4 dz_ip
        u16 tunnel_id
        NetworkV4 tunnel_net
    }

    Device --> Location : location_pk
    Device --> Exchange : exchange_pk
    Tunnel --> Device : side_a_pk
    Tunnel --> Device : side_z_pk
    User --> Device : device_pk
```

## User Types in DoubleZero

The DoubleZero protocol defines three distinct types of users:

- **Foundation:** Responsible for administering locations and exchanges. The foundation manages the core infrastructure and governance of the network.
- **Network Contributors:** Responsible for administering devices and tunnels. Contributors expand and maintain the network by adding and managing hardware and connectivity.
- **Users:** End users who connect to the DoubleZero network with a User account. These users consume network services and resources provided by the contributors and foundation.

These roles are enforced at the protocol level and reflected in the permissions and operations available to each user type.

## Location Commands

The following commands allow you to manage Location entities on the DoubleZero protocol. Each command is represented by a struct with the listed arguments.

### CreateLocationCommand
Creates a new location with the specified parameters. Returns the transaction signature and the location's public key on success.
- `code: String` — Unique location code
- `name: String` — Location name
- `country: String` — Country code
- `lat: f64` — Latitude
- `lng: f64` — Longitude
- `loc_id: Option<u32>` — Optional location ID

### UpdateLocationCommand
Updates the parameters of an existing location. Returns the transaction signature.
- `index: u128` — Location index
- `code: Option<String>` — Optional new code
- `name: Option<String>` — Optional new name
- `country: Option<String>` — Optional new country
- `lat: Option<f64>` — Optional new latitude
- `lng: Option<f64>` — Optional new longitude
- `loc_id: Option<u32>` — Optional new location ID

### GetLocationCommand
Fetches a location by its public key or code. Returns the location's public key and its on-chain data if found.
- `pubkey_or_code: String` — Location public key or code

### DeleteLocationCommand
Deletes a location by index. Returns the transaction signature.
- `index: u128` — Location index

### ListLocationCommand
Lists all locations in the program. Returns a map of location public keys to their on-chain data.
- *(no arguments)*

| Field         | Type    | Description                |
|-------------- |---------|----------------------------|
| pda_pubkey    | Pubkey  | PDA public key (primary key) |
| code          | String  | Unique location code       |
| name          | String  | Location name              |
| country       | String  | Country code               |
| lat           | f64     | Latitude                   |
| lng           | f64     | Longitude                  |
| loc_id        | u32     | Location ID                |

### ResumeLocationCommand
Resumes a previously suspended location by index. Returns the transaction signature.
- `index: u128` — Location index

## Exchange Commands

The following commands allow you to manage Exchange entities on the DoubleZero protocol. Each command is represented by a struct with the listed arguments.

### CreateExchangeCommand
Creates a new exchange with the specified parameters. Returns the transaction signature and the exchange's public key on success.
- `code: String` — Unique exchange code
- `name: String` — Exchange name
- `lat: f64` — Latitude
- `lng: f64` — Longitude
- `loc_id: Option<u32>` — Optional location ID

### UpdateExchangeCommand
Updates the parameters of an existing exchange. Returns the transaction signature.
- `index: u128` — Exchange index
- `code: Option<String>` — Optional new code
- `name: Option<String>` — Optional new name
- `lat: Option<f64>` — Optional new latitude
- `lng: Option<f64>` — Optional new longitude
- `loc_id: Option<u32>` — Optional new location ID

### GetExchangeCommand
Fetches an exchange by its public key or code. Returns the exchange's public key and its on-chain data if found.
- `pubkey_or_code: String` — Exchange public key or code

### DeleteExchangeCommand
Deletes an exchange by index. Returns the transaction signature.
- `index: u128` — Exchange index

### ListExchangeCommand
Lists all exchanges in the program. Returns a map of exchange public keys to their on-chain data.
- *(no arguments)*

| Field         | Type    | Description                |
|-------------- |---------|----------------------------|
| pda_pubkey    | Pubkey  | PDA public key (primary key) |
| code          | String  | Unique exchange code       |
| name          | String  | Exchange name              |
| lat           | f64     | Latitude                   |
| lng           | f64     | Longitude                  |
| loc_id        | u32     | Location ID                |

### CreateExchangeCommand
Creates a new location with the specified parameters. Returns the transaction signature and the location's public key on success.
- `code: String` — Unique exchange code
- `name: String` — Exchange name
- `lat: f64` — Latitude
- `lng: f64` — Longitude
- `loc_id: Option<u32>` — Optional location ID

## Device Commands

The following commands allow you to manage Device entities on the DoubleZero protocol. Each command is represented by a struct with the listed arguments.

### CreateDeviceCommand
Creates a new device with the specified parameters. Returns the transaction signature and the device's public key on success.
- `code: String` — Unique device code
- `location_pk: Pubkey` — Location public key
- `exchange_pk: Pubkey` — Exchange public key
- `device_type: DeviceType` — Device type enum
- `public_ip: IpV4` — Public IPv4 address
- `dz_prefixes: NetworkV4List` — List of DoubleZero prefixes

### UpdateDeviceCommand
Updates the parameters of an existing device. Returns the transaction signature.
- `index: u128` — Device index
- `code: Option<String>` — Optional new code
- `device_type: Option<DeviceType>` — Optional new type
- `public_ip: Option<IpV4>` — Optional new public IP
- `dz_prefixes: Option<NetworkV4List>` — Optional new prefixes

### GetDeviceCommand
Fetches a device by its public key or code. Returns the device's public key and its on-chain data if found.
- `pubkey_or_code: String` — Device public key or code

### DeleteDeviceCommand
Deletes a device by index. Returns the transaction signature.
- `index: u128` — Device index

### ListDeviceCommand
Lists all devices in the program. Returns a map of device public keys to their on-chain data.
- *(no arguments)*

| Field         | Type           | Description                |
|-------------- |---------------|----------------------------|
| pda_pubkey    | Pubkey         | PDA public key (primary key) |
| code          | String         | Unique device code         |
| device_type   | DeviceType     | Device type enum           |
| location_pk   | Pubkey         | Location public key        |
| exchange_pk   | Pubkey         | Exchange public key        |
| public_ip     | IpV4           | Public IPv4 address        |
| dz_prefixes   | NetworkV4List  | List of DoubleZero prefixes|

### CloseAccountDeviceCommand
Closes the device account, releasing its resources. Returns the transaction signature.
- `index: u128` — Device index
- `owner: Pubkey` — Owner public key

### RejectDeviceCommand
Rejects a device by index, providing a reason. Returns the transaction signature.
- `index: u128` — Device index
- `reason: String` — Rejection reason

### ResumeDeviceCommand
Resumes a previously suspended device by index. Returns the transaction signature.
- `index: u128` — Device index

## Tunnel Commands

The following commands allow you to manage Tunnel entities on the DoubleZero protocol. Each command is represented by a struct with the listed arguments.

### CreateTunnelCommand
Creates a new tunnel between two endpoints with the specified parameters. Returns the transaction signature and the tunnel's public key on success.
- `code: String` — Unique tunnel code
- `side_a_pk: Pubkey` — Public key for side A
- `side_z_pk: Pubkey` — Public key for side Z
- `tunnel_type: TunnelTunnelType` — Tunnel type enum
- `bandwidth: u64` — Bandwidth in bps
- `mtu: u32` — MTU size
- `delay_ns: u64` — Delay in nanoseconds
- `jitter_ns: u64` — Jitter in nanoseconds

### GetTunnelCommand
Fetches a tunnel by its public key or code. Returns the tunnel's public key and its on-chain data if found.
- `pubkey_or_code: String` — Tunnel public key or code

### RejectTunnelCommand
Rejects a tunnel by index, providing a reason. Returns the transaction signature.
- `index: u128` — Tunnel index
- `reason: String` — Rejection reason

### ResumeTunnelCommand
Resumes a previously suspended tunnel by index. Returns the transaction signature.
- `index: u128` — Tunnel index

### DeleteTunnelCommand
Deletes a tunnel by index. Returns the transaction signature.
- `index: u128` — Tunnel index

### ActivateTunnelCommand
Activates a tunnel, assigning it a tunnel ID and network. Returns the transaction signature.
- `index: u128` — Tunnel index
- `tunnel_id: u16` — Tunnel ID
- `tunnel_net: NetworkV4` — Tunnel network (IPv4)

### CloseAccountTunnelCommand
Closes the tunnel account, releasing its resources. Returns the transaction signature.
- `index: u128` — Tunnel index
- `owner: Pubkey` — Owner public key

### ListTunnelCommand
Lists all tunnels in the program. Returns a map of tunnel public keys to their on-chain data.
- *(no arguments)*

| Field         | Type              | Description                |
|-------------- |------------------|----------------------------|
| pda_pubkey    | Pubkey            | PDA public key (primary key) |
| code          | String            | Unique tunnel code         |
| side_a_pk     | Pubkey            | Public key for side A      |
| side_z_pk     | Pubkey            | Public key for side Z      |
| tunnel_type   | TunnelTunnelType  | Tunnel type enum           |
| bandwidth     | u64               | Bandwidth in bps           |
| mtu           | u32               | MTU size                   |
| delay_ns      | u64               | Delay in nanoseconds       |
| jitter_ns     | u64               | Jitter in nanoseconds      |
| tunnel_id     | u16               | Tunnel ID                  |
| tunnel_net    | NetworkV4         | Tunnel network (IPv4)      |

### UpdateTunnelCommand
Updates the parameters of an existing tunnel. Returns the transaction signature.
- `index: u128` — Tunnel index
- `code: Option<String>` — Optional new code
- `tunnel_type: Option<TunnelTunnelType>` — Optional new type
- `bandwidth: Option<u64>` — Optional new bandwidth
- `mtu: Option<u32>` — Optional new MTU
- `delay_ns: Option<u64>` — Optional new delay
- `jitter_ns: Option<u64>` — Optional new jitter

### SuspendTunnelCommand
Suspends a tunnel, disabling its operation without deleting it. Returns the transaction signature.
- `index: u128` — Tunnel index

## User Commands

The following commands allow you to manage User entities on the DoubleZero protocol. Each command is represented by a struct with the listed arguments.

### CreateUserCommand
Creates a new user with the specified parameters. Returns the transaction signature and the user's public key on success.
- `user_type: UserType` — User type enum
- `device_pk: Pubkey` — Device public key
- `cyoa_type: UserCYOA` — CYOA type enum
- `client_ip: IpV4` — User client IPv4 address

### UpdateUserCommand
Updates the parameters of an existing user. Returns the transaction signature.
- `index: u128` — User index
- `user_type: Option<UserType>` — Optional new user type
- `cyoa_type: Option<UserCYOA>` — Optional new CYOA type
- `client_ip: Option<IpV4>` — Optional new client IP
- `dz_ip: Option<IpV4>` — Optional new DoubleZero IP
- `tunnel_id: Option<u16>` — Optional new tunnel ID
- `tunnel_net: Option<NetworkV4>` — Optional new tunnel network

### GetUserCommand
Fetches a user by its public key or code. Returns the user's public key and its on-chain data if found.
- `pubkey_or_code: String` — User public key or code

### DeleteUserCommand
Deletes a user by index. Returns the transaction signature.
- `index: u128` — User index

### ListUserCommand
Lists all users in the program. Returns a map of user public keys to their on-chain data.
- *(no arguments)*

| Field         | Type         | Description                |
|-------------- |-------------|----------------------------|
| pda_pubkey    | Pubkey      | PDA public key (primary key) |
| user_type     | UserType    | User type enum             |
| device_pk     | Pubkey      | Device public key          |
| cyoa_type     | UserCYOA    | CYOA type enum             |
| client_ip     | IpV4        | User client IPv4 address   |
| dz_ip         | IpV4        | DoubleZero IP              |
| tunnel_id     | u16         | Tunnel ID                  |
| tunnel_net    | NetworkV4   | Tunnel network (IPv4)      |

### SuspendUserCommand
Suspends a user, disabling their access without deleting the account. Returns the transaction signature.
- `index: u128` — User index

### ResumeUserCommand
Resumes a previously suspended user by index. Returns the transaction signature.
- `index: u128` — User index

### CloseAccountUserCommand
Closes the user account, releasing its resources. Returns the transaction signature.
- `index: u128` — User index
- `owner: Pubkey` — Owner public key

### RejectUserCommand
Rejects a user by index, providing a reason. Returns the transaction signature.
- `index: u128` — User index
- `reason: String` — Rejection reason

