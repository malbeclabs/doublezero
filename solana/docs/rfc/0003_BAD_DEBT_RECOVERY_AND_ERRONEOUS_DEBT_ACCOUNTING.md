# Bad Debt Recovery and Erroneous Debt Accounting

---

## Summary

The proposed feature is to allow the following handling of written-off debt:

1. Recover bad debt if Solana validators pay back the debt that the protocol
   wrote off. If a Solana validator expresses his intent of wanting to be
   connected to the DoubleZero network by paying off his bad debt, there is
   currently no way for the network contributors to benefit.
2. Record erroneously calculated debt. There may be unforeseen circumstances
   which result in the accountant to miscalculate debt for a given Solana
   validator. By recording this debt as erroneous, the protocol effectively
   forgives this debt due to its own error.

With the new feature, this debt recovery will provide a windfall to network
contributors for a future rewards distribution.

## Motivation

Currently, there are only two pathways to handle Solana validator debt: pay and
write-off. Paid debt goes towards network contributor rewards. Debt write-offs
help the protocol by relieving the system from accruing bad debt, which reduces
the rewards for a given distribution.

If there were no mechanism to write off bad debt, the protocol will eventually
not be able to distribute rewards as soon as the rewards deferral period ends.
But the protocol does not benefit from keeping this debt written off without any
recourse to recoup these losses.

There are two pathways on how to handle written-off debt.

1. Attempt to collect this debt to recover the losses incurred from the past.
2. Forgive this debt if the protocol accrued this debt by mistake.

The second pathway is effectively no different than keeping the debt written-off
as-is, but it clears up whether a Solana validator is still on the hook to pay.
The first pathway, though, is crucial for rewarding network contributors.

Network contributors may project future revenue based on written-off debt. With
debt recovery, these projections will more accurately reflect the
creditworthiness of users on the network.

Once rewards have been distributed for a given distribution, there is nothing
left to do with that distribution. 2Z tokens swept into those distributions
effectively finalize the amount of rewards that will be distributed to network
contributors and burned. Because the distribution has reached the end of its
lifecycle, recovered debt will act as a windfall for a future distribution. A
future distribution’s swept 2Z amount will be determined by the sum of collected
debt for that distribution and the recovered debt from a past distribution.

## New Terminology

Debt recovery is a new term introduced to the protocol.  This new term defines
the mechanism of providing a windfall for a future distribution based on paying
back past bad debt. The term also captures the ability to improve a Solana
validator’s standing in the protocol by showing how reliable he is paying off
his debts.

## Alternatives Considered

Debt calculations for a given distribution can account for any to-be-recovered
bad debt. This method requires the accountant to know (or guess) the amount a
Solana validator intends to pay back by monitoring this validator’s deposit
account balance. Monitoring Solana validator deposit balances introduces a
significant offchain burden and is prone to error. Even if the Solana validator
were to attach a memo with a particular transfer to his deposit account, the
memo and the deposit balance may not agree because both of these instructions
are independently constructed.

Debt recovery does not have to be implemented at all. If recovery were not
considered, all written-off debt is effectively forgiven, which should not be
the end state of this debt. Solana validator creditworthiness will forever be
tarnished, which is also not an accurate reflection of their ability to pay
their dues.

## Detailed Design

### Onchain

There should be another field introduced to the distribution account schema,
which tracks the amount of recovered SOL debt.

```rust
pub struct Distribution {
    ..
    /// The amount of SOL that was accrued from a past distribution, but was
    /// written off. This amount is added to the total debt for this
    /// distribution and acts as a windfall for network contributors.
    recovered_sol_debt: u64,
    ..
}
```

The following method will change to incorporate this recovered SOL debt.

```rust
    pub fn checked_total_sol_debt(&self) -> Option<u64> {
        self.total_solana_validator_debt
            .saturating_add(self.recovered_sol_debt)
            .checked_sub(self.uncollectible_sol_debt)
    }
```

Instead of tracking with the existing `written_off_sol_debt`, where the recovery
process can reduce this value, there should also be another field introduced to
the Solana validator deposit account schema, which tracks the amount of
recovered SOL debt separately. Additionally, a field to track erroneous debt
should be added.

```rust
pub struct SolanaValidatorDeposit {
    ..
    /// The amount of SOL that was accrued from a past distribution, but was
    /// written off.
    recovered_sol_debt: u64,
    
    /// The amount of SOL that was erroneously calculated by the protocol.
    erroneous_sol_debt: u64,
    ..
}
```

Recoverable debt would then be calculated as:

```rust
    pub fn checked_recoverable_sol_debt(&self) -> Option<u64> {
        self.written_off_sol_debt
            .saturating_sub(self.recovered_sol_debt)
            .checked_sub(self.erroneous_sol_debt)
    }
```

Two instructions should be introduced.

```rust
pub enum RevenueDistributionInstructionData {
    ..
    RecoverBadSolanaValidatorDebt {
        amount: u64,
        proof: MerkleProof,
    },
    ConfigureSolanaValidatorDeposit(SolanaValidatorDepositConfiguration),
    ..
}

pub enum SolanaValidatorDepositConfiguration {
    ErroneousSolDebt(u64),
}
```

The recover-bad-solana-validator-debt instruction arguments are similar to the
pay-solana-validator-debt instruction. This instruction can only be called by
the debt accountant because he will determine which distributions to prioritize
for debt recovery. It may be possible to make this instruction permissionless in
the future. This instruction requires the following accounts:

1. Program config. The instruction will check whether the program is paused. If
it is, force the instruction to revert.
2. Debt accountant. This account’s key will be checked against the debt
accountant key encoded in the program config. This account must be a signer,
which enforces that the debt accountant is calling this instruction. If the
instruction becomes permissionless, this account will not be checked.
3. Distribution with bad debt. This account will have the debt write-off bitmap
to validate whether the Solana validator has debt written off for this
distribution. The bitmap’s value at this Solana validator’s index will be set to
false by the time the instruction succeeds. The distribution account must have
already had rewards finalized or the instruction will revert.
4. Solana validator deposit account. This account will have its
`recovered_sol_debt` increase by the amount of debt recovered. Its node ID along
with the instruction data will be used to construct the leaf to compute the
merkle root, which will be verified against the debt merkle root in the
distribution with bad debt. If the roots do not agree, the instruction will
revert. The instruction should also revert if the recoverable debt calculated
using this deposit account is not at least as much as the amount passed into
this instruction.
5. Journal. This account will receive the SOL from the Solana validator deposit
account for the recovered debt amount. This SOL transfer is consistent with the
pay-solana-validator-debt instruction.
6. Distribution for windfall. This account will have its `recovered_sol_debt`
increase by the amount of debt recovered. This distribution’s debt merkle root
must be finalized and rewards root must not be finalized (otherwise the
instruction will revert). This distribution does not have to have any debt
computed for it; recovered debt is additive, so the total collected debt would
then result in swept 2Z for rewards.

The configure-solana-validator-deposit instruction will have only one possible
value associated with this proposal: erroneous SOL debt. There may be more ways
to configure a Solana validator deposit account in the future, where additional
configuration options can be added to the above enum. This instruction requires
the following accounts:

1. Program config. The instruction will check whether the program is paused. If
it is, force the instruction to revert.
2. Debt accountant. This account’s key will be checked against the debt
accountant key encoded in the program config. This account must be a signer,
which enforces that the debt accountant is calling this instruction.
3. Solana validator deposit account. The erroneous SOL debt will be updated
based on the value passed into the instruction. If the configured erroneous
amount exceeds the difference between written-off and recovered debt, the
instruction should revert.

Ensure program logs write enough information consistent with other instruction
processors.

### Offchain

The debt accountant offchain process should introduce a programmatic way of
trying to recover debt. For every new epoch, it should attempt to recover debt
from the earliest distribution with written-off debt. The easiest way to
determine whether the process should recover debt is by fetching the Solana
validator deposit balances for all node IDs who have debt calculated and
checking whether the balance of each exceeds the calculated debt for a given
epoch. This debt recovery will apply to the next distribution whose rewards will
be distributed at the turn of the next epoch.

The debt accountant should also add a command to specify erroneous debt
attributed to a Solana validator’s node ID. This command simply invokes the configure-solana-validator-deposit instruction while specifying an amount for
erroneous SOL debt.

## Impact

Because the proposal adds instructions handling written-off debt only, these
instructions are isolated from the existing Revenue Distribution smart contract
functionality. The offchain changes required to handle debt recovery should
precede a smart contract upgrade, which will support the new instructions.

The debt accountant offchain process will require more logic when it runs. The
programmatic debt recovery can occur immediately after initializing a new
distribution to ensure it runs once per epoch. Or it can be implemented more
simply by running asynchronously by trying to recover debt at a fixed interval
(but this method will require more cycles to run).

There should be no impact to offchain performance.

This change will increase the expected value of rewards for network contributors
by providing a windfall of 2Z tokens as a result of recovering bad debt. And the
community can use the written-off, recovered and erroneous debt amounts to
determine creditworthiness of Solana validators.

## Security Considerations

This change does not introduce new attack surfaces. Both instructions are
guarded by checking that the caller is the debt accountant.

The erroneous debt amount may reveal bugs in tracking Solana validator users,
either in the debt accounting process or at the DoubleZero network layer.

## Backward Compatibility

Everything is backwards compatible because no existing interfaces change. The
existing debt accountant offchain process can continue running while the debt
recovery logic is added.

This change only requires a minor version bump if a major version is established
or a patch version bump if zero-versioned.

## Open Questions

- Can debt recovery be introduced as a permissionless instruction? How can the
Revenue Distribution program ensure that the recover-bad-solana-validator-debt
instruction is called at the right time if anyone can call it?
- Should erroneous debt be flagged at the distribution level using a bitmap
similar to tracking processed and written-off debt?