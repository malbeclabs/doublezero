## Smartcontract Development Best Practices

### Security

1. **PDA ownership verification**: Always verify the owner of PDA accounts (both internal PDAs and those from other programs like serviceability) to prevent being tricked into reading an account owned by another program. For serviceability accounts, verify the owner is the serviceability program ID. For your own PDAs, verify the owner is `program_id`.

2. **System program validation**: Checks for the system program are unnecessary because the system interface builds instructions using the system program as the program ID. If the wrong program is provided, you'll get a revert automatically.

3. **PDA validation**: When validating PDAs with expected seeds/bumps, you don't need to separately check the account derivation before the PDA validation - the PDA validation itself confirms the derivation is correct.

### Error Handling

1. **Simplify error enum conversions**: Use `#[repr(u32)]` on your error enum and implement `From<YourError> for ProgramError` using `as u32`. This eliminates the need to manually maintain error code mappings when adding new variants. Remove `Custom(u32)` variants unless there's a specific use case.

2. **Clear error messages**: Error messages should clearly state what condition is expected, not just what failed. For example, use "Cannot delete GeoProbe. reference_count of {n} > 0" instead of "ReferenceCountNotZero" so users understand what needs to be true.

### Code Organization

1. **Instruction struct placement**: Place instruction argument structs in the same file where the instruction is implemented, rather than collecting them all in a central `instructions.rs` file. This improves locality and makes it easier to understand what arguments an instruction uses.

2. **Minimize stored data**: Don't store bump seeds unless the account needs to sign for something. Bump seeds are only needed for CPI signing, not for PDA validation.

3. **Avoid redundant instruction arguments**: If you're passing an account, don't also pass that account's pubkey as an instruction argument and then check they match. Just use the account's key directly.

### Serialization

1. **Prefer standard derives**: Use `BorshDeserialize` when possible instead of implementing custom deserialization. Custom `unpack()` methods that manually match on instruction indices often duplicate what Borsh's derive already provides.

2. **Use BorshDeserializeIncremental**: For instruction arguments that may gain new optional fields over time, use `BorshDeserializeIncremental` or derive `BorshDeserialize`.

### Program Upgrades

1. **Use standard interfaces**: Use `solana-loader-v3-interface` to parse `UpgradeableLoaderState` rather than implementing your own parser. The interface crate provides well-tested, maintained implementations.

