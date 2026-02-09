# RFC-15: Simultaneous Publisher and Subscriber Multicast

## Summary

**Status: Draft**

Allow a client to be both a multicast publisher and subscriber simultaneously.

## Motivation

Validators need to both publish and consume data over multicast. The current implementation enforces mutual exclusion at the daemon and CLI layers. Since both directions already work over a single GRE tunnel, the restriction is artificial — removing it is a targeted change.

## Design

### Daemon

When a client connects with both publisher and subscriber groups, the publisher path runs first (creates tunnel with IP, advertises DZ IP via BGP, adds pub routes), then the subscriber path reuses the existing tunnel and adds sub routes and PIM. If only one mode is requested, behavior is unchanged.

The daemon does not support updating an existing multicast service. If a multicast service is already provisioned, a second provision call is rejected. Both publisher and subscriber roles must be specified in a single connect command.

Teardown already handles both correctly — PIM cleanup is gated on subscriber state, tunnel/BGP teardown always runs.

### CLI

New `--pub-groups` and `--sub-groups` flags on `doublezero connect multicast`:

```bash
# New syntax — both roles in a single command
doublezero connect multicast --pub-groups group-a --sub-groups group-b

# Publisher or subscriber only
doublezero connect multicast --pub-groups group-a
doublezero connect multicast --sub-groups group-b

# Legacy syntax (unchanged)
doublezero connect multicast publisher group-a
doublezero connect multicast subscriber group-b
```

Mixing legacy positional args with the new flags is an error. The CLI converts legacy syntax internally.

The CLI checks whether the daemon already has an active multicast service before proceeding. If one exists, it fails early with a message to disconnect first and reconnect with all desired groups. This prevents the user from hitting the daemon's rejection after onchain state has already been modified.

## Limitations

Incremental role addition (e.g., connect as publisher, then later add subscriber without disconnecting) is not supported. The daemon does not support updating an in-flight multicast service, so both roles and all groups must be declared upfront in a single connect command. Incremental connect support is left for future implementation.

## Backward Compatibility

- Legacy CLI syntax unchanged
- Daemon provisioning API unchanged (`MulticastPubGroups`/`MulticastSubGroups` were already separate fields)
- Smart contract already supports a user in both `publishers` and `subscribers` lists
