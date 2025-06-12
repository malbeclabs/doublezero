# Supporting Multiple Tunnels

## Summary

DoubleZero needs to support multiple tunnels of the same or different types. In order to terminate multiple tunnels to the same DZD from a user machine, a unique pair of (source, destination) tunnel IP endpoints is required for the Linux kernel to correctly demux traffic. This document defines the changes necessary to allow this and bootstraps the ability to begin storing network interface metadata which is necessary for upcoming work. The outcome of this work would be a new on-chain interfaces table, minimally populated with interfaces used for tunnel termination and all subsequent systems referring to this table to derive tunnel endpoints as opposed to the `public_ip` field of the devices table.

## Motivation

Multiple tunnel support is required now that DoubleZero supports IBRL and multicast. In fact, multicast can not be publicly released without multiple tunnel support due to restrictions in the Linux kernel.

## New Terminology

## Alternatives Considered

1. **Only support one tunnel on a user's machine.** In the current DoubleZero architecture, we're unable to support both unicast and multicast forwarding on a single tunnel. This would require a user to make a choice between using DoubleZero for unicast traffic or multicast traffic, which is not a user friendly tradeoff.

2. **Require users to obtain a second public address.** While this would satisfy the requirement of a unique (source, destination) tunnel IP endpoint per tunnel, it pushes this issue back on the users of DoubleZero and possibly prevents user uptake at the expense of more engineering work.

3. **Adapt the devices table in the current smart contract to fit a second tunnel (i.e. multicast) endpoint.** While this seems like significantly less work on its face, we end up needing to touch the same portions of the stack as a more ideal solution as they all need to be taught to understand this field.

## Detailed Design

TBX

### Data Structure Changes

A new data structure, `Interface`, will be defined that is attached to a parent `Device`'s public key. The relationship between an `Interface` and `Device` is many-to-one.

```mermaid
classDiagram
    class Interface {
      AccountType account_type
      Pubkey owner
      Pubkey device_pk
      string name
      string device_type
      IpV4Inet ip4_addr
      bool tunnel_termination
    }
    class Device {
        AccountType account_type
        Pubkey owner
        u128 index
        u8 bump_seed
        Pubkey location_pk
        Pubkey exchange_pk
        DeviceType device_type
        IpV4 public_ip
        DeviceStatus status
        String code
        NetworkV4List dz_prefixes
    }

    Interface --> Device : device_pk
````

### Network Changes
IPs will be assigned from a general pool of IP addresses. These IP addresses will be originally sourced from the IPs that the contributors provide through their minimum /29. These IPs are already used to assign src IPs for multicast tunnels. There is a limited supply of IPs that will be exhausted somewhat quickly. To mitigate the IP resource problem, DoubleZero can either request more IPs from network contributors or if necessary, IPs can be pulled from the /21 that DoubleZero owns. These are being set aside for edge filtration so they should only be used if absolutely necessary.

### Service Changes

#### CLI
1. The CLI currently selects the tunnel termination endpoint for a user connection based on min(latency) across all DZDs. In the event there is an existing tunnel terminated on the DZD, we need to select the next best endpoint on the same DZD.
2. Users need to be able perform CRUD operations on the on-chain interfaces i.e. `doublezero interface create`.
3. Users need to be able to display interfaces listed on-chain via `doublezero interfaces list` or some derivative command.

#### Daemon
Latency probing changes are needed for this as the current implementation looks at the public_ip field of device record to probe each DZD:
  1. Look at device table and then the interface table based on the device pubkey
  2. Filter on tunnel termination interfaces per device
  3. Initiate latency probes per tunnel termination
  4. Store results as <Device: Interface: LatencyResult> and serve via /latency endpoint for CLI

#### Activator
* Logic for assigning an IP will need to be modified to account for `n` > 1 IPs instead of just the first IP available
* Smart contract will need to be amended to associate `n` > 1 interfaces with a particular device
* Initial bootstrapping of a device may have to be revisited


#### Controller
* *optional*: configuration for tunnel termination loopbacks generated in device template
* *optional*: migrate logic from ansible into the controller to reduce the need for ansible

## Security Considerations

While this RFC introduces the concept of more tunnels, the same security mechanisms are in place that guard against unauthorized actors through the allowlist generated through the smart contract. If there are security vulnerabilities, they exist for any and all tunnels.

There is more information exposed on-chain, namely the `interface` struct. Perhaps someone could use that information to put together a fuller picture of a contributor's topology, but network contributors are providing resources that will be used in an open and transparent way so this is likely not an issue.


## Backwards Compatibility

New logic will introduce a breaking change as this RFC covers the initial rollout of multicast. This release will be tagged with a minor version of 0.2.0 to signify the breaking change.

## Open Questions
* While not necessary for this initial multiple tunnels RFC, should logic be added to the controller to start handling some of the ansible functionality?
* Updating the smart contract seems non-trivial; must it be this way or are there things that can reduce the friction to smart contract changes?
* What kind of data validation / sanitization is required to ensure that bad data isn't entered? In a SQL db, indexes can be used (or am ORM) to ensure data confirms but not sure what kind of on-chain validation can or should be done.
* Should a user be able to provide their own "termination point" or should it be assigned by DoubleZero? To start, it makes sense to not allow this but is this functionality that a user would want?
