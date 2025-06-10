# Supporting Simultaneous Tunnels

## Summary

DoubleZero needs to support simultaneous tunnels of the same or different types.

## Motivation

Simultaneous tunnel support is required now that DoubleZero supports IBRL and multicast.
## New Terminology

*

## Alternatives Considered



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

* We could start the migration away from Ansible to do configuration and move more toward putting logic in the controller.
*

Discuss effects on:

* Existing codebase (modules touched, refactors required)
* Operational complexity (deployment, monitoring, costs)
* Performance (throughput, latency, resource usage)
* User experience or documentation
  Quantify impacts where possible; note any expected ROI.

## Security Considerations

*Threat analysis and mitigations.*
Identify new attack surfaces, trust boundaries, or privacy issues introduced by the change. Describe how each risk is prevented, detected, or accepted and reference relevant best practices.

## Backwards Compatibility

New logic will introduce a breaking change as this RFC covers the initial rollout of multicast. This release will be tagged with a minor version of 0.2.0 to signify the breaking change.

## Open Questions






## NOTES

* unique endpoint per tunnel


## Network
* define pool of addresses (dz prefix on chain)
    * pull two addresses off
    * pull x number of ips per pool
    * /21 - generic DZ block, from which /24s are carved out
        * should keep for edge filtration
    * every contributor allocates a min of /29, but ideally more
        * already used to assign src IPs for multicast tunnels
        * ask for more address space
* mechanism to assign interface to second address
* test to make sure you can terminate a tunnel on a secondary address
    * reserve a range of loopbacks n-y for tunnel termination loopbacks

* network metadata on chain (interfaces et al)
* fk to relate network metadata to a particular "thing" (on chain)


## Client
* look at device struct and then the "table" (above)
* daemon probes capable endpoints on device (currently public ip) and would need to point and probe every tunnel option
* CLI asks for latency results then looks to see if tunnel is already present, if so take the next available one
* IPs are service agnostic

## activator
* check to see logic around which dz prefixes are chosen
* logic that change; currently first ip now we need >1 and changes reflected on the table
* initial device bootstrapping needs to be considered
    * where/how do you allocate
    * can a user set their own termination "points" etc

## controller
* interface logic?
* get logic into controller and out of ansible


## Smart Contract
* pool of ips from which you could terminate the tunnel (IPs tenant/VRF agnostic)
* IPs terminate in the default VRF
* activator already keeps track of client ips, p2p, etc
* register device, allocate prefixes
