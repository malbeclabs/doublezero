//! Stake-mirror instruction builders.
//!
//! One instruction, and one caller: the relayer that copies a builder's Solana stake onto the DZ
//! ledger. It routes through `authorize(.., STAKE_ORACLE)`, so it belongs on the
//! permission-appending path like feed and topology.
//!
//! `STAKE_ORACLE` differs from its neighbours in one way worth knowing here: no legacy
//! `GlobalState` key satisfies it, so the Permission account is not an optimisation for this
//! instruction, it is the only way a caller is ever authorized. A builder that omitted it would
//! produce a transaction that cannot succeed on any cluster.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_stake_mirror_pda},
    processors::stake_mirror::write::StakeMirrorWriteArgs,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

use crate::common;

/// `WriteStakeMirror` (variant 119). Accounts: `[stake_mirror, globalstate]`.
///
/// The mirror PDA derives from `args.stake_ref`, the `builder-stake` account being mirrored, not
/// from the builder: RFC-28 collateralizes each feed on its own bond, and a builder-keyed address
/// would let one bond back two feeds.
pub fn write_stake_mirror(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: StakeMirrorWriteArgs,
) -> Instruction {
    let (stake_mirror, _) = get_stake_mirror_pda(program_id, &args.stake_ref);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::WriteStakeMirror(args),
        vec![
            AccountMeta::new(stake_mirror, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::state::stake_mirror::StakeTier;

    use super::*;

    fn args(stake_ref: Pubkey, builder: Pubkey) -> StakeMirrorWriteArgs {
        StakeMirrorWriteArgs {
            stake_ref,
            builder,
            tier: StakeTier::UpTo1Gbps,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 42,
        }
    }

    /// The mirror account comes first and derives from the stake, and the payer and system program
    /// follow, which is the shape every other permission-gated builder produces.
    #[test]
    fn test_write_stake_mirror_accounts() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let stake_ref = Pubkey::new_unique();

        let ix = write_stake_mirror(&program_id, &payer, args(stake_ref, Pubkey::new_unique()));

        assert_eq!(ix.program_id, program_id);
        assert_eq!(
            ix.accounts[0].pubkey,
            get_stake_mirror_pda(&program_id, &stake_ref).0
        );
        assert!(ix.accounts[0].is_writable);
        assert_eq!(ix.accounts[1].pubkey, get_globalstate_pda(&program_id).0);
        assert_eq!(ix.accounts[2].pubkey, payer);
        assert!(ix.accounts[2].is_signer);
        assert_eq!(ix.accounts[3].pubkey, solana_system_interface::program::ID);
    }

    /// Keyed on the stake, not the builder. Two stakes held by one builder mirror to two accounts,
    /// which is what makes RFC-28's one feed per stake enforceable.
    #[test]
    fn test_the_mirror_derives_from_the_stake_not_the_builder() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let builder = Pubkey::new_unique();

        let first = write_stake_mirror(&program_id, &payer, args(Pubkey::new_unique(), builder));
        let second = write_stake_mirror(&program_id, &payer, args(Pubkey::new_unique(), builder));

        assert_ne!(first.accounts[0].pubkey, second.accounts[0].pubkey);
    }

    /// The tag byte is 119 and the args follow it, taken from `pack` rather than hand-written.
    #[test]
    fn test_wire_encoding_round_trips() {
        let program_id = Pubkey::new_unique();
        let expected = args(Pubkey::new_unique(), Pubkey::new_unique());

        let ix = write_stake_mirror(&program_id, &Pubkey::new_unique(), expected.clone());

        assert_eq!(ix.data[0], 119);
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::WriteStakeMirror(actual) => assert_eq!(actual, expected),
            other => panic!("expected WriteStakeMirror, got {other:?}"),
        }
    }
}
