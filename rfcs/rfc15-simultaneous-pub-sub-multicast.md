# RFC-15: Simultaneous Publisher and Subscriber Multicast

## Summary

**Status: Draft**

Allow a client to be both a multicast publisher and subscriber simultaneously.

## Motivation

Validators need to both publish and consume data over multicast. The current implementation enforces mutual exclusion at the daemon and CLI layers. Since both directions already work over a single GRE tunnel, the restriction is artificial — removing it is a targeted change.

## Design

### Daemon

When a client connects with both publisher and subscriber groups, the publisher path runs first (creates tunnel with IP, advertises DZ IP via BGP, adds pub routes), then the subscriber path reuses the existing tunnel and adds sub routes and PIM. If only one mode is requested, behavior is unchanged.

The daemon does not support updating an existing multicast service. If a multicast service is already provisioned, a second provision call is rejected. Both publisher and subscriber roles must be specified in a single connect command. To change roles or groups, the client must disconnect first and reconnect with the full desired configuration.

Teardown already handles both correctly — PIM cleanup is gated on subscriber state, tunnel/BGP teardown always runs.

### CLI

New `--publish` and `--subscribe` flags on `doublezero connect multicast`:

```bash
# Both roles in a single command
doublezero connect multicast --publish group-a --subscribe group-b

# Publisher or subscriber only
doublezero connect multicast --publish group-a
doublezero connect multicast --subscribe group-b

# Multiple groups per role
doublezero connect multicast --publish group-a group-b group-c
doublezero connect multicast --subscribe group-x group-y

# Multiple groups with both roles
doublezero connect multicast --publish group-a group-b --subscribe group-x group-y

# Same group as both publisher and subscriber
doublezero connect multicast --publish group-a --subscribe group-a

# Legacy syntax (unchanged)
doublezero connect multicast publisher group-a
doublezero connect multicast subscriber group-b
```

Mixing legacy positional args with the new flags is an error. The CLI converts legacy syntax internally.

The CLI checks whether the daemon already has an active multicast service before proceeding. If one exists, it fails early with a message to disconnect first and reconnect with all desired groups. This prevents the user from hitting the daemon's rejection after onchain state has already been modified.

## Limitations

Incremental role or group addition is not supported. For example, you cannot connect as a publisher and then later add a subscriber role (or additional publisher groups) without disconnecting first. The daemon does not support updating an in-flight multicast service, so all roles and all groups must be declared upfront in a single connect command. To change the set of groups or roles, disconnect and reconnect:

```bash
# This does NOT work — the second command will be rejected
doublezero connect multicast --publish group-a
doublezero connect multicast --subscribe group-b  # Error: multicast service already running

# Instead, specify everything in one command
doublezero connect multicast --publish group-a --subscribe group-b

# Or disconnect and reconnect to change configuration
doublezero disconnect multicast
doublezero connect multicast --publish group-a --subscribe group-b
```

Incremental connect support is left for future implementation.

## Backward Compatibility

- Legacy CLI syntax unchanged
- Daemon provisioning API unchanged (`MulticastPubGroups`/`MulticastSubGroups` were already separate fields)
- Smart contract already supports a user in both `publishers` and `subscribers` lists
