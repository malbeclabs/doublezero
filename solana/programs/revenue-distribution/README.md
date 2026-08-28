# Revenue Distribution Program

## Operations

### Finalizing an epoch whose integration cannot be collected

`FinalizeDistributionRewards` rejects a null rewards merkle root while 2Z is
still owed to contributors: the distribution has nonzero collected 2Z, or a
registered integration has not been collected yet. Finalize is permissionless
and one-way, so this guard is what prevents a premature finalize from
permanently stranding that 2Z.

An epoch can be structurally uncollectable. Example: a frozen idle epoch with
zero shred distributions, where the shreds integration has nothing to withdraw,
so `CollectIntegrationRewards` for that epoch can never succeed. The guard then
blocks the null-root finalize path for the epoch, and because the offchain
validator-debt worker packs `FinalizeDistributionRewards` for epoch N into the
same transaction as `InitializeDistribution` for epoch N+2, new distributions
stop initializing as well.

To resolve it, the rewards accountant posts a real (non-null) rewards merkle
root for the stuck epoch via `ConfigureDistributionRewards` (`total_contributors`
plus the root, signed by the rewards accountant in the program config). A
finalize with a real root does not require every integration to be collected,
so the pipeline resumes. This is safe for any 2Z that arrives later: rewards
splitting reads the distribution's collected 2Z total at distribute time, so an
integration collected after a rooted finalize still flows to contributors.
