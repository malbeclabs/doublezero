# Simultaneous Tunnels 

## Summary

**Status: `Draft`**

When the RFC has been implemented, a user will be able to have simultaneous tunnels. This will likely be a single unicast and a single multicast tunnel but it doesn't have to be. The previous work that has been done to enable multicast using PIM (protocol independent multicast) and RP (rendezvous point) will now be extended to enable multiple tunnel support.

Note that this RFC does not allow a user to become both a subscriber and a publisher for multicast feeds. That will be done in a subsequent RFC. The main reason for this bifurcation is that these changes are largely internal to the protocol and to the network whereas the multicast pub/sub changes are largely user-facing and perhaps more user feedback should be solicited or more thought about how the CLI should be enhanced, especially with other ongoing efforts like multi-tenancy and a push towards a single monorepo and CLI.


## Motivation

Currently, a user can have either a unicast or multicast tunnel but not both. This was an intentional limitation at the time of initial development biasing towards trials with external parties. Now that limitation needs to be removed so multicast can be more fully adopted. 

## New Terminology

* Rendezvous Point (RP) - 
* Protocol Independent Multicast (PIM) - 
* 

## Alternatives Considered


1. **Only support one tunnel on a user's machine.** In the current DoubleZero architecture, we're unable to support both unicast and multicast forwarding on a single tunnel. This would require a user to make a choice between using DoubleZero for unicast traffic or multicast traffic, which is not a user friendly tradeoff.

2. **Require users to obtain a second public address.** While this would satisfy the requirement of a unique (source, destination) tunnel IP endpoint per tunnel, it pushes this issue back on the users of DoubleZero and possibly prevents user uptake at the expense of more engineering work.
  
3.  **Use GRE keys to identify tunnels.** GRE keys enable a route to de-encapsulate packets and idenfity the right tunnel to use. This would have been a good approach except that at rates of about 250 Mbps, packets were being dropped which makes it unviable option.


## Detailed Design

*Exact technical specification.*
Provide enough detail for someone to implement the feature:

* Architecture overview (diagrams encouraged but optional)
* Data structures, schemas, or message formats
* Algorithms, control flow, or state machines
* API or CLI changes (with example calls)
* Configuration options, defaults, and migration steps
  Use subsections as needed; aim for clarity over brevity.

## Impact



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

