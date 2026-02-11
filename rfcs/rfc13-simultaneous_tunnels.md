# Simultaneous Tunnels 

## Summary

**Status: `Approved`**

When the RFC has been implemented, a user will be able to have simultaneous tunnels. This will be a single unicast and a single multicast-capable tunnel. The previous work that has been done to enable multicast using PIM (protocol independent multicast) and RP (rendezvous point) will now be extended to enable multiple tunnel support.

Note that this RFC does not include support for a user to become both a subscriber and a publisher for multicast feeds. That will be done in a subsequent RFC. The main reason for this bifurcation is that the changes in this RFC are largely internal to the protocol and network. In contrast, the multicast pub/sub changes are largely user-facing. More user feedback should be solicited, and more thought is needed about how the CLI should be enhanced, especially with other ongoing efforts like multi-tenancy and a push towards a single monorepo and CLI.

## Motivation

There are several desired use cases for DoubleZero that require the ability to transmit/receive both unicast traffic and multicast traffic. Currently, a user can have either a unicast or multicast tunnel but not both. Due to the implementation details of the IBRL mode of DoubleZero, which allows users to reuse their public IP addresses to communicate across the network, we're constrained in the ability to use a single tunnel interface to handle both unicast and multicast traffic. 

## New Terminology

N/A

## Alternatives Considered

1. **Only support one tunnel on a user's machine.** The ability to use a single tunnel for both unicast and multicast traffic would require the capability to support multicast signaling inside of the isolated routing table used for IBRL mode. There are a handful of unworkable ways to do this:
   - Use a GRE overlay internal to DoubleZero, which would require creating N X copies of multicast traffic across the core of DoubleZero
   - Utilize point-to-multicast RSVP-signaled label switch paths; While this would feasibly work, there are network platform limitations that constrain this to only 64 egress devices. There are currently 86 devices participating in DoubleZero.
   - Force the user to choose between a unicast or multicast tunnel

2. **Require users to obtain a second public address.** While this would satisfy the requirement of a unique (source, destination) tunnel IP endpoint per tunnel, it pushes this issue back on the users of DoubleZero and possibly prevents user uptake at the expense of more engineering work.
  
3.  **Use GRE keys to identify tunnels.** GRE keys enable tunnel endpoints with multiple tunnels between one another (same src/dst IP pair) to demultiplex packets into the correct tunnel interface. This is not currently supported on the hardware platforms used within DoubleZero.

## Out of Scope

* Support for publishing and subscriber over the same tunnel interface will be scoped as a 2nd phase of this work.
* Support for subscribing to multicast feeds over the same tunnel interface is not in scope for this work.

## Detailed Design

This design enables simultaneous tunnels by provisioning a second tunnel endpoint on each DZD device, allowing users to maintain both a unicast tunnel and a multicast tunnel concurrently. The approach requires changes across four areas:

1. **Activator**: Assigns multicast publishers an IP from a new global address pool (proposed: 147.51.126.0/23) rather than the per-device dz_prefix ranges, which are too small for anticipated multicast volume.

2. **Client**: Updates the CLI to provision a second tunnel by locating an available tunnel endpoint on the user's current device, or falling back to a nearby device within a 5ms latency threshold. The existing check preventing multiple tunnels will be removed.

3. **Serviceability**: Introduces multicast tunnel slot tracking on the smart contract to control multicast user capacity per device, with a default of 0 slots for newly added devices.

4. **Network**: Requires contributors to configure at least two public IPv4 addresses per device as tunnel endpoints, registered on the smart contract with the `user-tunnel-endpoint` tag. Separate max-user counts for unicast and multicast (default: 48 each) enable capacity planning.

### Application Changes

#### Activator
* If the user is a multicast publisher, the activator will need to assign a DoubleZero IP from the next available global pool listed in the global config portion of the serviceability program. The range of addresses used for this is TBD. 
* Multicast publishers should no longer be allocated addresses from the dz_prefix range tied to the device in the serviceability program. In order to accommodate the volume of multicast publishers for some use cases, these address blocks are not large enough (i.e. a /28 or /29 per device). While we could fill these pools up and spill into a global pool, this address space is routed over the public internet and it's desirable to reserve this range for edge filtering users.
* Initial bootstrapping of a device and enforcing the existence of the two tunnel endpoints will need to be revisited at a later time. 

#### Client
* During provisioning of either a second tunnel, the DoubleZero CLI will need to be taught the following. In all cases, devices with max tunnel slot counts of 0 should be excluded:
  1. Determine if the user has an existing tunnel to the device, and if so, attempt to find a second tunnel termination point in the interfaces list.
  2. If there are no additional tunnel endpoints on the device, find the next lowest latency device within a 5ms latency threshold.
  3. If no devices meet this criteria, return an error message to the user.
* doublezerod will need to remove the check in place to prevent an additional tunnel from being provisioned. Most of the implementation work to support simultaneous tunnels was done prior.

#### Controller
* We don't believe there is any controller work that needs to occur for this feature work as this is not scoped for being a multicast publisher/subscriber over the same tunnel interface. 

#### Serviceability
* Contributors need the ability to add a second publicly reachable IP per device as a loopback interface and flag the interface as a tunnel endpoint. We believe this support is already in place. 
* Because there is a desire to control the number of multicast users per device, the concept of multicast tunnel slots needs to be introduced. For every new user, this should be incremented. For every disconnecting user, this should be decremented. Devices should have a default value of 0 when added onchain.

### Network Changes
* There must be at least two public IPv4 addresses configured and reachable for DZ users over the CYOA interfaces of the DZD.  As an example, for CYOA interfaces of type `gre-over-dia`, the IP addresses must be routable over the Internet.
* On the smartcontract, these IP addresses should be registered as CYOA interfaces and tagged as `user-tunnel-endpoint`.   The `ip-net` flag should be used to allow contributors to assign their own address space.  
* `user-tunnel-endpoint` IP addresses should be configured on Loopback interfaces to reduce the risk of an outage if a physical interface goes down.  However, if there is only a single CYOA interface, a `user-tunnel-endpoint` may be configured on a physical interface.
* To support capacity planning, the smart-contract should support distinct max-user counts for unicast and multicast.  By default, both values should be set to 48.
* Policing of user-tunnels, particularly important for multicast tunnels, will be addressed in a separate RFC.
* Multicast publishers should be assigned an IP address from a global pool of address space defined in `global-config`.  It is proposed that this is set to 147.51.126.0/23, allowing 512 distinct multicast publishers.  If required to expand this block, there should be an option for including additional ranges.

## Impact

The immediate impact of implementing this RFC will be users with potentially two tunnels, a unicast and multicast tunnel. It's unlikely there will be a surge of users adding multicast tunnels. There are a handful of multicast publishers already on mainnet-beta who are admitting subscribers to their feed. This pattern of gradual adoption is likely to continue unless there's a sea change. Most multicast tunnels will likely be subscriber tunnels, subscribing to the existing publishers on the network. That could change, though, depending on what publishers or others bring to the network. 

Even though multicast is intelligently replicated throughout the network, it doesn't come without increased bandwidth in the network and must be monitored closely as the multicast usage increases. Rate limiting is an important guardrail, but in lieu of that, using the monitoring and alerting that currently exists, unexpected spikes can be ameliorated through existing alerts. We are working closely with the initial set of multicast publishers so that we understand usage patterns and general resource usage. 

This will be the first generally available feature that doesn't exist on the public internet. There are some expected use cases for multicast but there are likely to be some novel and unanticipated use cases which, in addition to the anticipated use cases, is a compelling and exciting reason to offer multicast generally. 

## Security Considerations

While this RFC introduces the concept of more tunnels, the same security mechanisms are in place that guard against unauthorized actors through the allowlist generated through the smart contract. If there are security vulnerabilities, they exist for any and all tunnels.

## Backward Compatibility

The current implementation's limitation of having only a unicast or multicast tunnel goes away so backward compatibility, such as it is, remains the same. This is additive functionality. 
