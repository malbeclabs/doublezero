# Instruction Implementation Guidelines

This document describes the required steps and best practices for implementing a new instruction in this Solana program. All developers must follow these guidelines to ensure correctness, security, and maintainability.

---

## 1. Account Parsing and Validation

- Parse all accounts in the order expected by the instruction.
- For each account, check:
  - **Ownership:** Ensure the account is owned by the expected program (usually `program_id`).
  - **Signer:** Verify that required accounts (e.g., payer) are signers.
  - **Writable:** Ensure accounts that will be mutated are marked as writable.
  - **System Program:** If the system program is required, check its address matches `solana_system_interface::program::ID`.
  - **PDA Validation:** If the instruction involves a PDA, derive the expected PDA and bump seed from internal state (e.g., GlobalState.account_index), then verify the provided account matches.

**Example:**
```rust
let mgroup_account = next_account_info(accounts_iter)?;
let globalstate_account = next_account_info(accounts_iter)?;
let payer_account = next_account_info(accounts_iter)?;
let system_program = next_account_info(accounts_iter)?;

assert!(payer_account.is_signer, "Payer must be a signer");
assert_eq!(globalstate_account.owner, program_id, "Invalid GlobalState Account Owner");
assert_eq!(*system_program.unsigned_key(), solana_system_interface::program::ID, "Invalid System Program Account Owner");
assert!(mgroup_account.is_writable, "PDA Account is not writable");

let mut globalstate = GlobalState::try_from(globalstate_account)?;
let (expected_pda_account, bump_seed) = get_multicastgroup_pda(program_id, globalstate.account_index + 1);
assert_eq!(mgroup_account.key, &expected_pda_account, "Invalid MulticastGroup Pubkey");
```

---

## 2. Input and Business Logic Validation

- Validate and normalize all input arguments (e.g., using helpers like `validate_account_code`).
- Deserialize account data using the appropriate helper (e.g., `try_from`).
- Validate any business-specific invariants (e.g., allowlist membership).
- Check for account initialization state as needed.

**Example:**
```rust
let code = validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
globalstate.account_index += 1;

if !globalstate.foundation_allowlist.contains(payer_account.key) {
    return Err(DoubleZeroError::NotAllowed.into());
}

if !mgroup_account.data_is_empty() {
    return Err(ProgramError::AccountAlreadyInitialized);
}
```

---

## 3. State Construction and Mutation

- Construct new state objects as needed, using validated and normalized data.
- Mutate in-memory state only after all checks and validations have passed.

**Example:**
```rust
let multicastgroup = MulticastGroup {
    account_type: AccountType::MulticastGroup,
    owner: value.owner,
    index: globalstate.account_index,
    bump_seed,
    tenant_pk: Pubkey::default(),
    code,
    multicast_ip: std::net::Ipv4Addr::UNSPECIFIED,
    max_bandwidth: value.max_bandwidth,
    status: MulticastGroupStatus::Pending,
    publisher_count: 0,
    subscriber_count: 0,
};
```

---

## 4. State Serialization and Account Creation

- Use the approved helpers (e.g., `try_acc_create`, `try_acc_write`) to create and write account data.
- Pass all required seeds and bump seeds for PDA creation.
- Ensure the payer is authorized to pay for the write if required.

**Example:**
```rust
try_acc_create(
    &multicastgroup,
    mgroup_account,
    payer_account,
    system_program,
    program_id,
    &[
        SEED_PREFIX,
        SEED_MULTICAST_GROUP,
        &globalstate.account_index.to_le_bytes(),
        &[bump_seed],
    ],
)?;
try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;
```

---

## 5. Error Handling

- Use explicit error returns for all failure cases.
- Avoid panics or unchecked unwraps in production code.
- Use descriptive error messages and custom error types where appropriate.

---

## 6. Logging (Optional for Tests)

- Use logging macros (e.g., `msg!`) for debugging and test builds as needed.

---

## 7. Return

- Return `Ok(())` on success.

---

## 8. Summary Checklist

- [ ] Parse and validate all accounts.
- [ ] Check signers, ownership, and PDA derivation.
- [ ] Validate and normalize input arguments.
- [ ] Deserialize and validate state.
- [ ] Check business logic and invariants.
- [ ] Construct and mutate state.
- [ ] Create and write account data.
- [ ] Handle errors explicitly.
- [ ] Log for debugging if needed.
- [ ] Return success.

---

By following these steps, you ensure that new instructions are secure, robust, and consistent with the rest of the codebase.
