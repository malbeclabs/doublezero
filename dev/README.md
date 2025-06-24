# Local Devnet

This lets you spin up and manage a DoubleZero devnet locally in containers. It includes the core components (ledger, activator, controller, devices, and clients) along with management tools, all running in isolation for repeatable and ad-hoc testing. It reuses the same underlying code as the [e2e](../e2e/) testing framework to provision and manage the local containerized devnet. You can interact with it using the `dzctl` CLI, exposed in this workspace as `dev/dzctl`.

## Getting Started

To start the devnet:

```sh
dev/dzctl start
```

This will build the docker images and start containers for the following components:
- **Ledger**
  - A Solana validator representing the DZ ledger, where the Serviceability and Telemetry programs are deployed.
- **Manager**
  - Contains the keys used to deploy programs, manage global config, and maintain allowlists.
  - Includes the `doublezero` CLI, `solana` CLI, and other tools for managing the network.
- **Activator**: The [doublezero-activator](../activator/) component.
- **Controller**: The [doublezero-controller](../controlplane/controller/) component.
- **Serviceability Program**
  - Deployed to the ledger.
  - Initialized via `doublezero init` from the manager container.
  - Seeded with `global-config`, `locations`, and `exchanges` from the manager container.

## Inspecting and Managing the Components

View running containers:
```sh
docker ps
```

Tail logs from a component:
```sh
docker logs -f dz-local-activator
```

Open a shell into a container:
```sh
docker exec dz-local-manager bash
```

## Stopping and Cleaning Up

To stop all containers without removing them:
```sh
dev/dzctl stop
```

To tear down all containers and associated volumes:
```sh
dev/dzctl destroy
```

## Adding Devices and Clients

Add a device ([agent](../controlplane/agent/)):

```sh
dev/dzctl add-device -v --code=lo-dz1 --cyoa-network-host-id 8
```

Add another device:

```sh
dev/dzctl add-device -v --code=lo-dz2 --cyoa-network-host-id 16
```

> ℹ️ Notes:
>
> - This command creates the device onchain in the local ledger if it doesn't already exist, using `doublezero device create` (run from the manager container).
> - You can verify with `doublezero device list`, or from the host:
>   ```sh
>   docker exec -it dz-local-manager doublezero device list
>   ```
> - The default allocatable prefix length is `/29`, yielding 8 IPs; hence the selection of `8` and `16` as the starting host IDs.
>

Add a client:

```sh
dev/dzctl add-client -v --cyoa-network-host-id 100
```

Add another client:
```sh
dev/dzctl add-client -v --cyoa-network-host-id 110
```

You can inspect their logs or shell into them using the same `docker logs` and `docker exec` methods.

## `dzctl` CLI Reference

For more advanced workflows, use `dev/dzctl` directly to access subcommands:

```console
$ dev/dzctl --help

Run a persistent local DoubleZero devnet locally in containers.

Usage:
  devnet [flags]
  devnet [command]

Available Commands:
  add-client      Create and start a client on the devnet
  add-device      Create and start a device on the devnet
  build           Build the docker images. This may take a minute or two
  completion      Generate the autocompletion script for the specified shell
  deploy-programs Deploy the Serviceability and Telemetry programs to the ledger
  destroy         Destroy the devnet and all its resources
  help            Help about any command
  start           Start the core devnet components; ledger, manager, activator, and controller
  start-ledger    Start the ledger if it's not already running. This command won't start the devnet if it's not already running
  stop            Stop all components in the devnet, including devices and clients

Flags:
      --deploy-id string   deploy identifier (env: DZ_DEPLOY_ID, default: dz-local) (default "dz-local")
  -h, --help               help for devnet
  -v, --verbose            set debug logging level

Use "devnet [command] --help" for more information about a command.
```

## Testing Client Connectivity

Enter a client container:

```
docker exec -it dz-local-client-8ZfopvXqytUFcDjnJ9xBoPe1Vwfn1sRxrzvEUsXuDBgH bash
```

Inside the container, check status:

```console
$ doublezero status
DoubleZero Service Provisioning
 Tunnel status | Last Session Update | Tunnel Name | Tunnel src | Tunnel dst | Doublezero IP | User Type
 disconnected  | no session data     |             |            |            |               |

$ doublezero address
AGyfdsiJoVHA33sQKSyWiV6b1ztbE13enmVwN7VujtQg

$ ip route
default via 172.23.0.1 dev eth0
10.169.90.0/24 dev eth1 proto kernel scope link src 10.169.90.100
172.23.0.0/16 dev eth0 proto kernel scope link src 172.23.0.7
```

Before connecting, add the client’s public key to the allowlist from the manager:
```console
# On your host machine:
$ docker exec -it dz-local-manager bash

# On the manager container:
$ doublezero user allowlist add --pubkey AGyfdsiJoVHA33sQKSyWiV6b1ztbE13enmVwN7VujtQg
Signature: pgeeBhuaXnsLqNCUzgp2m4qhSFyKzBcTwGj3TiFK3EsG66nM1SY3SXotazxwpxuwbKth6e3qC1MG452M5eBdCAD

$ doublezero user allowlist add --pubkey FaJJJgFiTUm92dLNA5tJuyRFQTLVmqD2xY3K7rqQ3259
Signature: w3jfVPkR9iHFoG7sGtAKJa9Bcb6uGX1Ynymjg8zVzhbxaUcCXw2wfoPyJJKsRDNZwgQMn2BNXwEcj1yFWjZQEhE
```

Now, back in the client container, connect:
```console
$ doublezero connect ibrl --client-ip 10.169.90.100 --allocate-addr
DoubleZero Service Provisioning
🔗  Start Provisioning User...
    Using Public IP: 10.169.90.100
🔍  Provisioning User for IP: 10.169.90.100
    Creating an account for the IP: 10.169.90.100
    The Device has been selected: lo-dz1
    User activated with dz_ip: 10.169.90.9
    User activated with dz_ip: 10.169.90.9
Provisioning: status: ok
/  Connected

$ doublezero status
DoubleZero Service Provisioning
 Tunnel status | Last Session Update     | Tunnel Name | Tunnel src    | Tunnel dst  | Doublezero IP | User Type
 up            | 2025-06-22 17:29:31 UTC | doublezero0 | 10.169.90.100 | 10.169.90.8 | 10.169.90.9   | IBRLWithAllocatedIP
```

✅ Note: The `--client-ip` flag is required because the `doublezero` CLI uses `ifconfig.me` to determine the client's public IP. Inside containers, this will return the host IP, which causes provisioning to fail. Use the explicit client IP instead.

Do the same thing on the other client and check the status:
```console
$ doublezero connect ibrl --client-ip 10.169.90.110 --allocate-addr
DoubleZero Service Provisioning
🔗  Start Provisioning User...
    Using Public IP: 10.169.90.110
🔍  Provisioning User for IP: 10.169.90.110
    Creating an account for the IP: 10.169.90.110
    The Device has been selected: lo-dz1
    User activated with dz_ip: 10.169.90.10
    User activated with dz_ip: 10.169.90.10
Provisioning: status: ok
/  Connected

$ doublezero status
DoubleZero Service Provisioning
 Tunnel status | Last Session Update     | Tunnel Name | Tunnel src    | Tunnel dst  | Doublezero IP | User Type
 up            | 2025-06-22 17:30:31 UTC | doublezero0 | 10.169.90.110 | 10.169.90.8 | 10.169.90.10  | IBRLWithAllocatedIP

$ doublezero user list
 account                                      | user_type           | groups | device | location | cyoa_type  | client_ip     | dz_ip        | tunnel_id | tunnel_net     | status    | owner
 GSXANjXYLZm8VvDiNWhPXf26XmxqZC1G7CayjtK2MHUQ | IBRLWithAllocatedIP |        | lo-dz1 | New York | GREOverDIA | 10.169.90.100 | 10.169.90.9  | 500       | 169.254.0.0/31 | activated | AGyfdsiJoVHA33sQKSyWiV6b1ztbE13enmVwN7VujtQg
 CqWRUGaKmaf3PANCZUkV8XKu7E79x2NDNfoEzD3mkL2v | IBRLWithAllocatedIP |        | lo-dz1 | New York | GREOverDIA | 10.169.90.110 | 10.169.90.10 | 501       | 169.254.0.2/31 | activated | FaJJJgFiTUm92dLNA5tJuyRFQTLVmqD2xY3K7rqQ3259
```

Test traffic flow between them on the allocated IPs:
```console
$ ping -c1 10.169.90.9
PING 10.169.90.9 (10.169.90.9) 56(84) bytes of data.
64 bytes from 10.169.90.9: icmp_seq=1 ttl=63 time=0.361 ms

--- 10.169.90.9 ping statistics ---
1 packets transmitted, 1 received, 0% packet loss, time 0ms
rtt min/avg/max/mdev = 0.361/0.361/0.361/0.000 ms
```
