# Improved Debt Write-Off Tracking

---

## Summary

The proposed feature is to add more account data for ease of uncollectible debt
tracking. Currently, it requires fetching transaction data to search for this
information, which is inefficient. Instead, this information should be added to
account data to avoid having to spend resources on RPC calls and indexing.

## Motivation

It is currently possible to fetch information of whose debt is written off and
how much. But doing so requires fetching every transaction, which will become
more like finding a needle in a haystack as more validators connect to the
DoubleZero network.

For example, a distribution would have a number of transactions roughly equal to
the number of Solana validators that owe debt towards this distribution. So if
there are 400 validators, we should expect at least 400 transactions associated
with this distribution PDA. 

If one of these transactions is a debt write off, we would have to first fetch
all transaction hashes associated with this distribution account. Then, we would
have to fetch details for each transaction to deserialize the instruction data
to find which transactions are associated with the debt write off instruction.
If we do not save this information offchain, we would have to perform the same
search mission.

This fetch could be condensed into one account fetch where all of this data
would basically be indexed for us. There would be no need for an offchain
indexer since it lives onchain.

The cost of keeping track of a bitmap of written off debt is 0.00000696 SOL
multiplied by the number of Solana validators divided by 8. This translates to a
very small amount of money that the accountant has to outlay to store this
information onchain.

If someone wanted to track the history of bad debt, the number of fetches is a
function of the number of distributions. Better yet, all distributions in
existence can be fetched by using `getProgramAccounts` with a filter for the
distribution account’s data discriminator, which is just one call.

## New Terminology

There is no new terminology introduced.

## Alternatives Considered

Data can be saved with an indexer, where this indexer would listen to
transactions and save the transaction hashes (or transaction drains) associated
with writing off debt, keyed off by the distribution’s epoch. But this process
would be offchain, meaning that folks would have to rely on a third party for
this information.

An SDK can be written that demonstrates how to fetch these transactions on
demand. But these fetches can be costly depending on the RPC rates and may have
a limit to how far back in time the RPC has access to archival data.

## Detailed Design

There should be two index fields added to the distribution accounts schema to
point to where in the account data to find the bitmap of debt write offs. The
Solana validator deposit account will track the total amount of debt written
off.

The forgive-solana-validator-debt instruction should change to take the Solana
validator deposit account as writable so the amount of written off debt can be
updated. The protocol can also deprecate this instruction in favor of another
instruction that writes off debt.

```rust
let solana_validator_deposit = ZeroCopyMutAccount::<SolanaValidatorDeposit>::try_next_accounts(
    &mut accounts_iter,
    Some(&ID),
)?;
msg!("Node ID: {}", solana_validator_deposit.node_id);
```

Currently the node ID is read in via instruction data. Its interface should
change to resemble the pay-solana-validator-debt instruction, where the node ID
can be read from the deposit account.

```rust

pub enum RevenueDistributionInstructionData {
    ..
    WriteOffSolanaValidatorDebt {
        amount: u64,
        proof: MerkleProof,
    },
    ..
}
```

An instruction to reallocate data to the distribution account based on the
number of Solana validators who have debt should be be introduced. This
instruction can be permissionless. The new instruction will check if the debt
write off indices have been set yet. If not, set them and reallocate the
account.

```rust
pub enum RevenueDistributionInstructionData {
    ..
    AllowSolanaValidatorDebtWriteOff,
    ..
}
```

This new instruction requires the System program in order to transfer lamports
from the invoker in order to make the Distribution account rent-exempt after the
data reallocation.

## Impact

Only one existing interface is affected by this change, but its instruction can
only be called by the debt accountant. The offchain process to handle writing
off debt will have to change to factor the additional account (Solana validator
deposit) and the change in instruction data.

If there is any written off debt executed before this change, the new debt
write-off bitmap and deposit account write-off tracking will not reflect
reality. In this case, offchain processes will have to fallback to fetching
transactions to resolve whose debt was written off and how much. The protocol
can consider an account migration after upgrading the smart contract to sync the
account states.

Performance in fetching written off debt information will be improved since this
data will now live onchain. This change will also improve debt accounting
transparency.

## Security Considerations

This change does not introduce new attack surfaces. Although the new account
data makes the debt accounting processing more transparent, there are no privacy
issues introduced since all of the data is onchain still (via fetching
transaction details).

## Backward Compatibility

If the debt accountant already handles debt write-offs, the offchain process
will have to account for the instruction interface change. Everything else is
backwards compatible.

Because this change introduces an interface change, the smart contract version
necessitates a major bump if a major version is established or a minor version
bump if zero-versioned.

## Open Questions

No open questions.