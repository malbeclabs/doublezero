# RFC-15: Simultaneous Publisher and Subscriber Multicast

## Summary

**Status: Draft**

Allow a client to be both a multicast publisher and subscriber simultaneously.

## Motivation

Validators need to both publish and consume data over multicast. The current implementation enforces mutual exclusion at the daemon and CLI layers. Since both directions already work over a single GRE tunnel, the restriction is artificial — removing it is a targeted change.

## Design

### Daemon

When a client connects with both publisher and subscriber groups, the publisher path runs first (creates tunnel with IP, advertises DZ IP via BGP, adds pub routes), then the subscriber path reuses the existing tunnel and adds sub routes and PIM. If only one mode is requested, behavior is unchanged.

Teardown already handles both correctly — PIM cleanup is gated on subscriber state, tunnel/BGP teardown always runs.

### CLI

New `--pub-groups` and `--sub-groups` flags on `doublezero connect multicast`:

```bash
# New syntax
doublezero connect multicast --pub-groups group-a --sub-groups group-b

# Legacy syntax (unchanged)
doublezero connect multicast publisher group-a
doublezero connect multicast subscriber group-b
```

Mixing legacy positional args with the new flags is an error. The CLI converts legacy syntax internally.

## Backward Compatibility

- Legacy CLI syntax unchanged
- Daemon provisioning API unchanged (`MulticastPubGroups`/`MulticastSubGroups` were already separate fields)
- Smart contract already supports a user in both `publishers` and `subscribers` lists
