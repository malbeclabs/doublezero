# Link Draining

## Summary

**Status: Active**

A frequent requirement of operating a physical network is the ability to remove a link from being an active part of the network topology.  This is often referred to as draining, where traffic is rerouted to alternative options or, in the case of provisioning, prevented from being actively used until formally declared Ready for Service (RFS).
 
The goal of this RFC is to define the use-cases and mechanisms for draining one or more links on a DZD.

Scenarios (workflows) where draining is used include:

* Provisioning a new DZD
* Provisioning a new link
* Scheduling maintenance activities
* Managing operational outages

## Motivation

It is critical that contributors have operational levers to manage the health of the network, particularly in relation to the scenarios outlined in the Summary section. For a blockchain project such as DZ, the goal must be to express this on a smart-contract, with automation to realize the desired intent. 

The existing tools that are available to achieve the equivalent of a draining status are described in table 1 below.

| Area                  | Options              | Owner                       | Description |
|------------------------|----------------------------|------------------------------|------------------------------|
| WAN Link |  1. Set delay on smart-contract to a high value i.e. 1000ms <br> 2. Shutdown WAN interface(s)  | 1. Contributor or DZF <br> 2. Contributor     | 1: Will migrate traffic to an alternative link only if available (soft draining).  Will not migrate traffic if link is only path on the network. <br> 2: Will force link offline  (hard draining).  Will reroute traffic to alternative if available.  Users will fall back to Internet if no alternatives are available.
| DZX Link | 1. Set delay on smart-contract to a high value i.e. 1000ms <br> 2. Shutdown WAN interface(s)      | 1. A-side contributor or DZF <br> 2. A-side or Z-side contributor    | 1: Will migrate traffic to an alternative link only if available (soft draining). Will not migrate traffic if link is only path on the network <br> 2: Will force link offline (hard draining).  Will reroute traffic to alternative if available.  Users will fall back to Internet if no alternatives are available.

Table 1: Existing Draining Options

The processes described in table 1 are fragile for a number of reasons.  Having different owners of different parts of the same workflow requires coordination between a contributor and the DZF, or, in the case of DZX links, between contributors themselves.  It is also not explicit the intent about setting increased link delay values.  Ultimately, a simple CLI option in the smart-contract that automates the multiple existing steps required with a new drained status helps operationally with ease of execution and the desired state of the network.  Additionally, a drained status can be used to support initial DZD provisioning and protecting the network during maintenance windows and outages.


## New Terminology

* Drained: the state where a link is removed from the active network topology.  A drained state could be applied to one or more links (WAN or DZX).
* Draining: the process of moving a link from activated to drained states
* Hard-drained: a link is removed from routing
* Soft-drained: a link IS-IS metric is set to 1,000,000, forcing traffic to use alternative paths only if available.  A soft-drained link will still be used by DZ users if it is the only path between two users
* Undraining: the process that reverses the draining process

## Alternatives Considered

* IS-IS overload bit: 
  * Risk of blackholing traffic 
  * Interaction with Segment Routing currently unknown

## Detailed Design

### WAN Link

* Update `link.status` field to include the following states: 
  * activated (existing):
    * Steady state
    * Available to forward traffic
    * IS-IS metric based on delay
  * hard_drained (new):
    * A link is removed from routing 
    * IS-IS disabled by removing `isis enable 1` on interfaces
    * Use-case: link maintenance or outage without alternatives available
  * soft_drained (new):
    * A link is deprioritized 
    * IS-IS metric is increased to 1,000,000: `isis metric 1000000`
    * Use-case: link maintenance or outage with alternatives (primary and secondary links) available

* Define a new delay_override field as part of the smart-contract definition of a link.
  * Supports an operator-defined mechanism to affect the use of a link
  * More granular control than soft_drained or hard_drained states
  * Is set to 0 by default
  * When a link is soft-drained, it will override both the delay and delay_override
  * Use-cases: 
    * link demoted from primary to secondary link, but is still preferred over tertiary link
* CLI Commands:
  * `doublezero link update --pubkey PUBKEY --status [hard_drained|soft_drained|activated]`
  * `doublezero link update --pubkey PUBKEY --delay-override-ms [0.01 <= X <= 1000]`

```mermaid
graph LR
    ACTIVATED[Activated] -->|Maintenance Started| HARD_DRAINED[Hard Drained]
    ACTIVATED -->|Link Prioritized/Deprioritized| SOFT_DRAINED[Soft Drained] 
    HARD_DRAINED -->|Maintenance Completed| SOFT_DRAINED 
    SOFT_DRAINED -->|Link Normalized/Routing Stable| ACTIVATED
```

### DZX Link

* Leverage same `link.status`and delay_override fields described in WAN Link section
* Update smart-contract to allow either A-Side or Z-Side contributors to trigger `link.status` transitions

### Verification
Appropriate verification should be implemented to ensure that a link has been successfully drained.  Additionally, appropriate verification should be implemented before a link can be set to `activated` (ink Normalized/Routing Stable).  This verification process will be detailed in a future RFC.

### Monitoring
Link status should be used to silence alerts in appropriate monitoring systems.  For example, a drained link should silence alerts related to Device Telemetry.

## Impact

This RFC should improve the operational controls to manage links in the network.  It introduces an intent based methodology that uses explicit fields to achieve the desired state.

The primary codebases that require updates include:
* serviceability
* controller

## Security Considerations

* The goal of this RFC is to automate existing workflows.  No new attack vectors should be introduced.

## Backward Compatibility

* Default values for all new smart-contract fields should be defined during the initial software release.
* Consideration should be given to links that have an outage and/or are currently overloading `delay-ms`

## Open Questions

* How do we determine it is safe to drain a DZD?
    * Capacity
    * Alternative routes
* Can we ensure that the agent can always talk to the controller if the CYOA is in a hard state?
* How do we want to manage monitoring when a DZD is set to drained?
