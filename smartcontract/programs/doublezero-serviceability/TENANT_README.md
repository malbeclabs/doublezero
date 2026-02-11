# Tenant Billing

Tenants pay for DoubleZero network access via flat-rate per-DZ-epoch billing. The billing sentinel automatically deducts 2Z tokens from each tenant's dedicated billing account and transfers them to the Journal ATA for revenue distribution.

## Onchain State

### `TenantBillingConfig`

```rust
pub enum TenantBillingConfig {
    FlatPerEpoch(FlatPerEpochConfig),
}

pub struct FlatPerEpochConfig {
    pub rate: u64,                    // 2Z lamports to deduct per DZ epoch
    pub last_deduction_dz_epoch: u64, // last epoch successfully deducted (idempotency guard)
}
```

- **Default**: `FlatPerEpoch { rate: 0, last_deduction_dz_epoch: 0 }` -- backward compatible, existing tenants behave as before (legacy balance-check only).
- **`rate > 0`**: enables the deduction path. The sentinel transfers `rate` lamports of 2Z from the tenant's billing account each DZ epoch.
- **`rate == 0`**: legacy balance-check path only (no deduction).

The `billing` field is appended to the `Tenant` struct and defaults safely via `unwrap_or_default()` during deserialization, so existing onchain tenants are unaffected.

### Instructions

| Instruction                        | Who                              | What                                                                                                                       |
| ---------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `UpdateTenant` (variant 89)        | Foundation allowlist             | Set `billing: Some(TenantBillingConfig::FlatPerEpoch { rate, last_deduction_dz_epoch: 0 })` to enable billing for a tenant |
| `UpdatePaymentStatus` (variant 93) | Sentinel authority or foundation | Set `payment_status` (Paid/Delinquent) and optionally bump `last_deduction_dz_epoch` after a successful deduction          |

## Token Account Ownership Model

The tenant's billing token account is a standard SPL token account on Solana whose **owner** (in SPL token terms) is the sentinel authority pubkey. This allows the sentinel to directly sign SPL `transfer_checked` instructions without CPI or PDA signing.

- Tenant creates the account with the sentinel as SPL token owner
- Tenant deposits 2Z into the account address (anyone can transfer _into_ any token account)
- Sentinel transfers _from_ the account to the Journal ATA, signing as the SPL token owner
- The account address is registered in `Tenant.token_account` on the DZ Ledger

## Tenant Token Account Setup

The 2Z mint is `J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd`.

### 1. Generate a keypair for the billing account address

```bash
solana-keygen new -o billing-account.json --no-bip39-passphrase
```

Note the public key -- this is the billing account address that will be registered on the DZ Ledger.

### 2. Create the token account with sentinel as owner

```bash
spl-token create-account \
  J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd \
  billing-account.json \
  --owner <SENTINEL_AUTHORITY_PUBKEY>
```

The `--owner` flag sets the SPL token owner to the sentinel authority. This grants the sentinel the ability to transfer tokens out of this account for billing deductions.

### 3. Deposit 2Z into the billing account

```bash
spl-token transfer \
  J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd \
  <AMOUNT> \
  <BILLING_ACCOUNT_ADDRESS>
```

Where `<BILLING_ACCOUNT_ADDRESS>` is the public key from step 1.

### 4. Register the billing account and enable billing on the DZ Ledger

Use the `UpdateTenant` admin instruction to set both the token account and billing config:

```
Tenant.token_account = <BILLING_ACCOUNT_ADDRESS>
Tenant.billing = FlatPerEpoch { rate: <RATE>, last_deduction_dz_epoch: 0 }
```

The foundation executes this via the CLI or SDK.

## Operational Notes

- The tenant can **top up** the billing account at any time by transferring more 2Z to the account address
- The tenant **cannot withdraw** from the billing account (only the sentinel authority, as the SPL token owner, can transfer out)
- If the balance falls below the per-epoch rate, the sentinel marks the tenant as `Delinquent`
- The `billing-account.json` keypair is only needed at creation time and can be discarded afterward (the account persists onchain)
- The sentinel catches up **one epoch at a time** -- if a tenant is multiple epochs behind, each poll cycle advances by one epoch
- `last_deduction_dz_epoch` on the `Tenant` account is the ultimate source of truth for idempotency
