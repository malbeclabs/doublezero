# Simultaneous Tunnels 

## Summary

**Status: `Draft`**

When the RFC has been implemented, a user will be able to have simultaneous tunnels. This will be a single unicast and a single multicast-capable tunnel. The previous work that has been done to enable multicast using PIM (protocol independent multicast) and RP (rendezvous point) will now be extended to enable multiple tunnel support.

Note that this RFC does not allow a user to become both a subscriber and a publisher for multicast feeds. That will be done in a subsequent RFC. The main reason for this bifurcation is that these changes are largely internal to the protocol and to the network whereas the multicast pub/sub changes are largely user-facing and perhaps more user feedback should be solicited or more thought about how the CLI should be enhanced, especially with other ongoing efforts like multi-tenancy and a push towards a single monorepo and CLI.


## Motivation

There are several desired use cases for DoubleZero that require the ability to transmit/receive both unicast traffic and multicast traffic. Currently, a user can have either a unicast or multicast tunnel but not both. Due to the implementation details of the IBRL mode of DoubleZero, which allows users to reuse their public IP addresses to communicate across the network, we're constrained in the ability to use a single tunnel interface to handle both unicast and multicast traffic. 

## New Terminology

* Rendezvous Point (RP) - 
* Protocol Independent Multicast (PIM) - 
* 

## Alternatives Considered


1. **Only support one tunnel on a user's machine.** The ability to use a single tunnel for both unicast and multicast traffic would require the capability to support multicast signaling inside of the isolated routing table used for IBRL mode. There are a handful of unworkable ways to do this:
- Use a GRE overlay internal to DoubleZero, which would require creating N X copies of multicast traffic across the core of DoubleZero
- Utilize point-to-multicast RSVP-signaled label switch paths; While this would feasibly work, there are network platform limitations that constrain this to only 64 egress devices. There are currently 86 devices participating in DoubleZero.
- Force the user to choose between a unicast or multicast tunnel

2. **Require users to obtain a second public address.** While this would satisfy the requirement of a unique (source, destination) tunnel IP endpoint per tunnel, it pushes this issue back on the users of DoubleZero and possibly prevents user uptake at the expense of more engineering work.
  
3.  **Use GRE keys to identify tunnels.** GRE keys enable tunnel endpoints with multiple tunnels between one another (same src/dst IP pair) to demultiplex packets into the correct tunnel interface. This is not currently supported on the hardware platforms used within DoubleZero


## Detailed Design

### Application Changes

#### Activator
* need to check to make sure that the 

#### Client
* remove limitation to have only a unicast or multicast tunnel
* check to make sure that the CLI output doesn't break (shouldn't)
#### Controller
*  update template to render the changes required for simultaneous tunnels 
*  validate that temnplate changes are able to be processed efficiently
#### Serviceability
* add ability to add more than one IP per contributor per device 
* check to see about loopback options

### Network Changes
* There must be at least two public IPv4 addresses configured and reachable for DZ users over the CYOA interfaces of the DZD.  As an example, for CYOA interfaces of type `gre-over-dia`, the IP addresses must be routable over the Internet.
* On the smartcontract, these IP addresses should be registered as CYOA interfaces and tagged as `user-tunnel-endpoint`.   The `ip-net` flag should be used to allow contributors to assign their own address space.  
* `user-tunnel-endpoint` IP addresses should be configured on Loopback interfaces to reduce the risk of an outage if a physical interface goes down.  However, if there is only a single CYOA interface, a `user-tunnel-endpoint` may be configured on a physical interface.
* To support capacity planning, the smart-contract should support distinct max-user counts for unicast and multicast.  By default, both values should be set to 48.
* Policing of user-tunnels, particularly important for multicast tunnels, will be addressed in a separate RFC.
* Multicast publishers should be assigned an IP address from a global pool of address space defined in `global-config`.  It is proposed that this is set to 147.51.126.0/23, allowing 512 distinct multicast publishers.  If required to expand this block, there should be an option for including additional ranges.

## Impact


This woudl 



*Consequences of adopting this RFC.*
Discuss effects on:

* Existing codebase (modules touched, refactors required)
* Operational complexity (deployment, monitoring, costs)
* Performance (throughput, latency, resource usage)
* User experience or documentation
  Quantify impacts where possible; note any expected ROI.

## Security Considerations

The security posture should largely remains the same. Unicast and multicast tunnels are already part of the baseline and have been through a security audit.  

## Backward Compatibility

The current implementation's limitation of having only a unicast or multicast tunnel goes away so backward compatibility, such as it is, remains the same. This is additive functionality. 



## Open Questions

