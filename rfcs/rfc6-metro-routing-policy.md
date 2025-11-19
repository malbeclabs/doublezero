# DoubleZero Metro Area Routing Policy

## Summary

**Status: Active**

This rfc proposes an optimization to network traffic flow within each DoubleZero exchange. Currently, DoubleZero sends /32 routes representing the `dz_ip`s of all connected users, regardless of where they are physically located. After this change, DoubleZero will only send /32 routes representing users who are NOT connected to the same DoubleZero exchange. For example, after this change, traffic between users connected to devices in the Frankfurt exchange will no longer traverse the DoubleZero network, and will instead traverse the local internet infrastructure in Frankfurt.

## Motivation

Due to longest match routing, when a DoubleZero host receives /32 routes from DoubleZero, that host routes all traffic to that remote host via the `doublezero0` interface. This is true even when the remote host is on the same subnet or in the same building. This results in increased latency for users, and resource utilization on the DoubleZero devices and network links, so this optimization will benefit both users and contributors.

## New Terminology

In this document, "metro area" means a geographic area. An "exchange" is defined in the DoubleZero serviceability program to represent the metro areas in which DoubleZero operates. These terms are interchangeable in this document. 

## Alternatives Considered

Long term, we would like a more comprehensive routing policy defined on-chain. In the short term, the BGP-community based design detailed below is a well established pattern for achieving our goal of optimizing metro area traffic flow. We also considered adding a new bgp_community field to the serviceability exchange, but since the exchange.loc_id field is not used, there are no plans to use it, and it is therefore technical debt, we'll take the opportunity to clean it up by repurposing it.

## Detailed Design

### Routing policy
For comparison, today the DZ network and DZDs have control-plane protection with the eBGP sessions facing users as follows:
- From the user:
    - Accept the user’s `dz_ip` only
        - Controlled using a prefix-list
    - A community `21682:1200` (`COMM-ALL_USERS`) is tagged inbound on all user eBGP sessions
- To the user:
    - Permit to the user only `dz_ip` addresses tagged with `COMM-ALL_USERS`
        - All `dz_ip` addresses are sent to all users by default

This RFC proposes the following instead:
- From the user:
    - Add ***additional*** community inbound on all user eBGP sessions
        - One community per metro (exchange) that represents all user IP addresses in the metro
        - Community range: 21682:10000-10999
            - Allows up to 1000 metros before a new range would need to be defined
        - Examples:
            - NYC: `COMM-NYC_USERS` 21682:10000
            - LON: `COMM-LON_USERS` 21682:10001
            - FRA: `COMM-FRA_USERS` 21682:10002
- To the user:
    - Deny local metro community
    - Permit `COMM-ALL_USERS`

### Routing policy configuration example

```
!NYC
route-map RM-USER-{{ ID }}-IN permit 10
   match ip address prefix-list PL-USER-{{ ID }}
   match as-path length = 1
   set community 21682:1200 21682:10000

ip community-list COMM-ALL_USERS permit 21682:1200
ip community-list COMM-NYC_USERS permit 21682:10000
ip community-list COMM-LON_USERS permit 21682:10001
ip community-list COMM-FRA_USERS permit 21682:10002

route-map RM-USER-{{ ID }}-OUT deny 10
   match community COMM-NYC_USERS
route-map RM-USER-{{ ID }}-OUT permit 20
   match community COMM-ALL_USERS
   
router 65342
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-IN in
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-OUT out
   
!LON
route-map RM-USER-{{ ID }}-IN permit 10
   match ip address prefix-list PL-USER-{{ ID }}
   match as-path length = 1
   set community 21682:1200 21682:10001

ip community-list COMM-ALL_USERS permit 21682:1200
ip community-list COMM-NYC_USERS permit 21682:10000
ip community-list COMM-LON_USERS permit 21682:10001
ip community-list COMM-FRA_USERS permit 21682:10002

route-map RM-USER-{{ ID }}-OUT deny 10
   match community COMM-LON_USERS
route-map RM-USER-{{ ID }}-OUT permit 20
   match community COMM-ALL_USERS
   
router 65342
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-IN in
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-OUT out

!FRA
route-map RM-USER-{{ ID }}-IN permit 10
   match ip address prefix-list PL-USER-{{ ID }}
   match as-path length = 1
   set community 21682:1200 21682:10002

ip community-list COMM-ALL_USERS permit 21682:1200
ip community-list COMM-NYC_USERS permit 21682:10000
ip community-list COMM-LON_USERS permit 21682:10001
ip community-list COMM-FRA_USERS permit 21682:10002

route-map RM-USER-{{ ID }}-OUT deny 10
   match community COMM-FRA_USERS
route-map RM-USER-{{ ID }}-OUT permit 20
   match community COMM-ALL_USERS
   
router 65342
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-IN in
   neighbor {{ IP_ADDRESS }} route-map RM-USER-{{ ID }}-OUT out
```

### BGP community tracking
This new policy requires that we define a 16-bit BGP community value in the range 10000-10999 that is unique to each exchange. We store the value in the serviceability smartcontract in bgp_community field of each exchange. Previously we had an unused 32-bit field called loc_id that we have split into two 16-bit fields named `bgp_community` and `unused`. (The `unused` field is needed to maintain the length of the structure. This field can be used for other purposes in the future.) We also add a field named `next_bgp_community` to serviceability global-config.

Here's how we manage exchange.bgp_community:
1. On create, serviceability 1) sets exchange.bgp_community to the current value of global-config.next_bgp_community and 2) increments global-config.next_bgp_community
1. Perform a range check in serviceability to ensure all bgp_community values are in the 10000-10999 range
1. In the doublezero `monitor` component, raise an alert when a device's exchange's community is outside the desired range or when a duplicate values exist
1. In case a bug leads to an out-of-range value or a duplicate, authorized users can use `doublezero exchange update --bgp-community` to manually fix the value

## Failure scenarios
### Duplicate community
If two exchanges have the same community assigned, they will be regarded as a single exchange by the routing policy. Traffic between hosts connected to these two exchanges will traverse the public internet instead of DoubleZero. This is not a catastrophic outcome since impacted user traffic will continue to work, it just route over the public internet instead of over DoubleZero. We will mitigate this issue with data validation in the serviceability smartcontract and/or the activator component.

## Impact

Existing codebase (modules touched, refactors required):
- serviceability: rename exchange.loc_id to exchange.bgp_community; add next_bgp_community to global-config and use it for auto-assignment; validate the value is in the desired range
- cli: remove --loc_id command line flag from `doublezero exchange create`
- controller: get exchange.bgp_community from serviceability and add it to state cache
- controller: update config template to implement intra-exchange routing policy

Operational complexity (deployment, monitoring, costs)
- The new routing policy is more complex, but there are no specific implications for operations

Performance (throughput, latency, resource usage)
- Reduces network resource utilization and improves latency between users in the same geographic location

User experience or documentation
- Users will not see any change other than optimized routing.

Quantify impacts where possible; note any expected ROI
- We have not done a comprehensive traffic analysis, but we do know that, due to the network effect, the greater the number of users connected to an exchange, the greater the amount of intra-exchange traffic will traverse the DoubleZero network. So this will have the greatest impact where we have the most user traffic.

## Security Considerations

This change does not introduce any new attack surface that we are aware of.

## Backward Compatibility

- A data migration is required when this change is released, to update the values of existing exchange.loc_id to be unique and in the 10000-10999 range.
    - This needs to be done once, and can be done any time before the controller code is released to each DoubleZero environment (devnet/testnet/mainnet-beta).
    - The migration can be done manually since the data set is not too large -- we currently have 29 exchange records in mainnet-beta. 
- The DoubleZero Foundation process for creating a new exchange must be modified to no longer include the --loc-id flag
- After the data migration and feature rollout are complete, there are no further backward compatibility issues

## Open Questions

- N/A