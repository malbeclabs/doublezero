# Allow Writing Off Debt for Same Distribution

---

## Summary

The proposed feature is to allow uncollectible debt accounting for a given
DoubleZero rewards distribution to apply to the same distribution for which the
debt was originally computed. Without this feature, uncollectible debt is pushed
to another future distribution’s accounting, which may not be fair to network
contributors.

## Motivation

With the current method to write off debt, two distribution accounts are
required to execute the instruction onchain: one, whose debt is uncollectible,
and another (future) one, which will modify its debt calculation to satisfy the
uncollectible debt amount from the first distribution. For example, if
distribution #1 has collected 99 of 100 SOL and does not expect the remaining 1
SOL to be collectible, this 1 SOL can be written off and attributed to the total
collectible amount of distribution #2.

Debt accounting is used to determine how much 2Z can be swept into the
distribution after this SOL revenue is converted to 2Z. Using the example above,
if distribution #2 has 105 SOL debt, the amount of 2Z that is allowed to be
converted for this distribution is this amount less the written off amount from
distribution #1, which means only 104 SOL is convertible to 2Z.

The mechanism was designed this way because the 2Z sweeping was meant to not be
blocked by delays in determining whether any debt is uncollectible. So while the
accountant process determines whether to write off debt, contributors for the
current distribution can still be rewarded without delay. This mechanism works
fine if the proportion of bad debt is small and changes in contributor reward
calculation differences between distributions are unremarkable. But this
scenario may not be accurate in situations like existing contributors adding
more valuable links or new contributors providing connectivity to the DoubleZero
network. In conjunction with a significant amount of bad debt, these new
contributions would not be rewarded sufficiently.

The protocol should strive for rewards to be as accurate as possible. By
attributing bad debt to the same distribution the debt was originally
calculated, we achieve this goal.

## New Terminology

There is no new terminology introduced.

## Alternatives Considered

The protocol can do nothing, which avoids a smart contract upgrade. As long as
the risk of large uncollectible debt is managed in a way that it gets smoothed
out across future distributions, the effect of this bad debt may not be felt as
much. But any large amount of debt from a given user can make smoothing more
difficult.

The protocol can also consider a way to remove swept 2Z from a distribution to
attribute to a future distribution. But this accounting is not straightforward
due to not knowing how much 2Z should be associated with any amount of bad debt
after the sweep.

## Detailed Design

There are two parts of the instruction processor that need to change in order to
allow same-distribution debt write-off.

In the `try_forgive_solana_validator_debt` processor, we need to change the
current logic:

```rust
if next_distribution.dz_epoch <= distribution.dz_epoch {
    msg!("Next distribution's epoch must be ahead of the current distribution's epoch");
    return Err(ProgramError::InvalidAccountData);
}
```

The change is simply:

```rust
if next_distribution.dz_epoch < distribution.dz_epoch {
    msg!("Next distribution's epoch must be at least the epoch of the current distribution");
    return Err(ProgramError::InvalidAccountData);
}
```

When the accountant invokes this instruction, he will pass in the same pubkey
for both distribution accounts. The rest of the checks that follow will still
apply (making sure that debt has been finalized and 2Z has not been swept yet).
Even though these checks would have been already performed on the first
distribution, the redundancy does not cost much more compute units.

The next change must occur in the `try_sweep_distribution_tokens` processor,
where we will need to check that the rewards calculation has been finalized:

```rust
// Make sure the distribution rewards calculation is finalized.
if !distribution.is_rewards_calculation_finalized() {
    msg!("Distribution rewards have not been finalized");
    return Err(ProgramError::InvalidAccountData);
}
```

This check can occur anywhere after deserializing the distribution account in
this processor.

This check constrains the 2Z sweep to only occur after the reward deferral
period, which gives the accountant time to write off debt. And since the rewards
finalization is a prerequisite for sweeping 2Z, this check can now be removed
from the distribute rewards instruction processor.

## Impact

Only three instruction processors will be modified (see Detailed Design), with
one of these modifications being the elimination of a redundant check caused by
this change.

Tests will be added or modified to support this change, specifically the
forgive-solana-validator-debt and sweep-distribution-tokens instructions.

This change will require an onchain upgrade.

The effects of this change will result in more accurate rewards calculated for
network contributors whenever the accountant needs to write off debt.

## Security Considerations

Without this change, sweeping 2Z isolated an attack surface to steal funds to
the distribution account level. Whenever these tokens are swept, the amount of
2Z reflecting the amount of rewards to distribute and burn reside in a token
account associated only with this distribution. By allowing the sweep to be
performed whenever there is sufficient 2Z liquidity to satisfy the total debt
of this distribution, the cost of the attack on the swap destination account
(where 2Z sits as a result of SOL/2Z conversions prior to being swept) is
minimized.

Now that finalizing rewards is a prerequisite to sweeping 2Z, at least the
amount that covers the deferral period will sit in the swap destination account,
making the financial incentive to attack the protocol a multiple of what it was
(determined by this deferral period). So if the deferral period were configured
to 32 epochs, for example, the financial incentive increases by 32 fold since
there would be 32 epochs of rewards sitting before they can finally be
distributed.

When this change is implemented, it should be assumed that there is no
vulnerability or loophole that would allow an attacker to take these funds. The
existing safeties and additional checks associated with this change will make it
unlikely that an attack would succeed.

## Backward Compatibility

The only compatibility change is associated with the timing of sweeping 2Z,
which will require the rewards finalization as a prerequisite. Because it is a
permissionless instruction, any offchain process trying to call this instruction
will encounter a revert if rewards have not been finalized.

Aside from that change, the smart contract will be backward compatible since no
instruction interfaces or account schemas will change.

## Open Questions

- Are there any other checks that need to be introduced? Or are there any
  existing checks that are not redundant?