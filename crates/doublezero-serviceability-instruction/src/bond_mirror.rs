//! Bond-mirror instruction builders.
//!
//! One instruction, and one caller: the relayer that copies a builder's Solana bond onto the DZ
//! ledger. It routes through `authorize(.., BOND_ORACLE)`, so it belongs on the
//! permission-appending path like feed and topology.
//!
//! `BOND_ORACLE` differs from its neighbours in one way worth knowing here: no legacy
//! `GlobalState` key satisfies it, so the Permission account is not an optimisation for this
//! instruction, it is the only way a caller is ever authorized. A builder that omitted it would
//! produce a transaction that cannot succeed on any cluster.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_bond_mirror_pda, get_globalstate_pda},
    processors::bond_mirror::write::BondMirrorWriteArgs,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

use crate::common;

/// `WriteBondMirror` (variant 119).
///
/// Returns `[bond_mirror, globalstate, payer, system_program, permission]`. The payer's
/// `Permission` account is required rather than optional for this instruction: nothing else
/// authorizes `BOND_ORACLE`, so it must exist onchain and grant the flag.
///
/// `globalstate` is read-only. The processor reads `feature_flags` and hands it to `authorize`,
/// and writes nothing, so marking it writable would take a lock it does not need.
///
/// The mirror PDA derives from `args.bond_ref`, the `builder-bond` account being mirrored, not
/// from the builder: RFC-28 collateralizes each feed on its own bond, and a builder-keyed address
/// would let one bond back two feeds.
pub fn write_bond_mirror(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: BondMirrorWriteArgs,
) -> Instruction {
    let (bond_mirror, _) = get_bond_mirror_pda(program_id, &args.bond_ref);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::WriteBondMirror(args),
        vec![
            AccountMeta::new(bond_mirror, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::state::bond_mirror::BondTier;

    use super::*;

    fn args(bond_ref: Pubkey, builder: Pubkey) -> BondMirrorWriteArgs {
        BondMirrorWriteArgs {
            bond_ref,
            builder,
            tier: BondTier::UpTo1Gbps,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 42,
        }
    }

    /// The mirror account comes first and derives from the bond, the payer and system program
    /// follow, and `globalstate` is read-only because the processor never writes it.
    #[test]
    fn test_write_bond_mirror_accounts() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let bond_ref = Pubkey::new_unique();

        let ix = write_bond_mirror(&program_id, &payer, args(bond_ref, Pubkey::new_unique()));

        assert_eq!(ix.program_id, program_id);
        assert_eq!(
            ix.accounts[0].pubkey,
            get_bond_mirror_pda(&program_id, &bond_ref).0
        );
        assert!(ix.accounts[0].is_writable);
        assert_eq!(ix.accounts[1].pubkey, get_globalstate_pda(&program_id).0);
        assert!(
            !ix.accounts[1].is_writable,
            "the processor only reads globalstate"
        );
        assert_eq!(ix.accounts[2].pubkey, payer);
        assert!(ix.accounts[2].is_signer);
        assert_eq!(ix.accounts[3].pubkey, solana_system_interface::program::ID);
        assert_eq!(ix.accounts[4], common::permission_meta(&program_id, &payer));
        assert_eq!(ix.accounts.len(), 5);
    }

    /// Keyed on the bond, not the builder. Two bonds held by one builder mirror to two accounts,
    /// which is what makes RFC-28's one feed per bond enforceable.
    #[test]
    fn test_the_mirror_derives_from_the_bond_not_the_builder() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let builder = Pubkey::new_unique();

        let first = write_bond_mirror(&program_id, &payer, args(Pubkey::new_unique(), builder));
        let second = write_bond_mirror(&program_id, &payer, args(Pubkey::new_unique(), builder));

        assert_ne!(first.accounts[0].pubkey, second.accounts[0].pubkey);
    }

    /// The tag byte is 119 and the args follow it, taken from `pack` rather than hand-written.
    #[test]
    fn test_wire_encoding_round_trips() {
        let program_id = Pubkey::new_unique();
        let expected = args(Pubkey::new_unique(), Pubkey::new_unique());

        let ix = write_bond_mirror(&program_id, &Pubkey::new_unique(), expected.clone());

        assert_eq!(ix.data[0], 119);
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::WriteBondMirror(actual) => assert_eq!(actual, expected),
            other => panic!("expected WriteBondMirror, got {other:?}"),
        }
    }
}
