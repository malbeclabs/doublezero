use doublezero_serviceability::{
    pda::{get_bond_mirror_pda, get_permission_pda},
    processors::bond_mirror::write::BondMirrorWriteArgs,
    state::{
        bond_mirror::BondTier,
        permission::{permission_flags, Permission, PermissionStatus},
    },
};
use doublezero_serviceability_instruction::bond_mirror::write_bond_mirror;
use solana_sdk::{account::Account, pubkey::Pubkey, signature::Signature};
use std::fmt;

use crate::DoubleZeroClient;

/// Why a caller's `Permission` account cannot exercise `BOND_ORACLE`.
///
/// Separated from the command so every reason can be tested against an account directly rather
/// than through a mocked client, and so a caller reading a failure is told which way it is short
/// rather than just that it is.
#[derive(Debug, PartialEq)]
pub enum BondOraclePreflight {
    /// No account at the PDA, or one the program does not own. Either way the payer holds nothing.
    NoPermissionAccount,
    /// An account exists at the address but is not a `Permission`.
    NotAPermission,
    /// A `Permission` that is suspended, or was never activated.
    NotActivated(PermissionStatus),
    /// An activated `Permission` that does not carry the flag.
    MissingBondOracle,
}

impl fmt::Display for BondOraclePreflight {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoPermissionAccount => write!(f, "has no Permission account"),
            Self::NotAPermission => {
                write!(
                    f,
                    "has an account at its Permission address that is not one"
                )
            }
            Self::NotActivated(status) => write!(f, "has a Permission that is {status}"),
            Self::MissingBondOracle => write!(f, "has a Permission without BOND_ORACLE"),
        }
    }
}

/// Whether `account` lets its owner exercise `BOND_ORACLE`.
///
/// Checks what `authorize` checks. Ownership alone is not enough: `authorize` also rejects a
/// `Permission` that is suspended or lacks the flag, so a preflight that stopped at ownership
/// would still send transactions that cannot succeed.
pub fn check_bond_oracle(
    account: Option<&Account>,
    program_id: &Pubkey,
) -> Result<(), BondOraclePreflight> {
    let account = account
        .filter(|a| &a.owner == program_id)
        .ok_or(BondOraclePreflight::NoPermissionAccount)?;

    let permission =
        Permission::try_from(&account.data[..]).map_err(|_| BondOraclePreflight::NotAPermission)?;

    if permission.status != PermissionStatus::Activated {
        return Err(BondOraclePreflight::NotActivated(permission.status));
    }
    if permission.permissions & permission_flags::BOND_ORACLE == 0 {
        return Err(BondOraclePreflight::MissingBondOracle);
    }

    Ok(())
}

/// Copy a builder's Solana bond onto the DZ ledger. The relayer's write.
#[derive(Debug, PartialEq, Clone)]
pub struct WriteBondMirrorCommand {
    /// The `builder-bond` account on Solana being mirrored. The PDA seed.
    pub bond_ref: Pubkey,
    pub builder: Pubkey,
    pub tier: BondTier,
    pub committed_rate_bits_per_sec: u64,
    /// The Solana slot these values were read at. The program refuses a write whose slot is not
    /// strictly newer than the stored one, so a caller must track what it last wrote.
    pub source_slot: u64,
}

impl WriteBondMirrorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (mirror_pubkey, _) = get_bond_mirror_pda(&program_id, &self.bond_ref);

        let ix = write_bond_mirror(
            &program_id,
            &payer,
            BondMirrorWriteArgs {
                bond_ref: self.bond_ref,
                builder: self.builder,
                tier: self.tier,
                committed_rate_bits_per_sec: self.committed_rate_bits_per_sec,
                source_slot: self.source_slot,
            },
        );

        // The builder already appends the payer's Permission PDA, whether or not it exists. For an
        // instruction a legacy `GlobalState` key can also authorize that is all it takes, but no
        // legacy key satisfies `BOND_ORACLE`, so anything short of a usable Permission account is
        // a transaction that cannot succeed on any cluster. Check it first, so the caller learns
        // which way it is short instead of paying for a transaction that fails.
        let (permission_pubkey, _) = get_permission_pda(&program_id, &payer);
        let account = client
            .get_multiple_accounts(vec![permission_pubkey])?
            .into_iter()
            .next()
            .flatten();

        check_bond_oracle(account.as_ref(), &program_id).map_err(|problem| {
            eyre::eyre!(
                "{payer} {problem} at {permission_pubkey}. WriteBondMirror needs BOND_ORACLE, \
                 and no GlobalState key grants it, so a Permission account is the only way to \
                 hold it."
            )
        })?;

        client.send_transaction(ix).map(|sig| (sig, mirror_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::state::accounttype::AccountType;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    use super::*;
    use crate::tests::utils::create_test_client;

    fn command(bond_ref: Pubkey, builder: Pubkey) -> WriteBondMirrorCommand {
        WriteBondMirrorCommand {
            bond_ref,
            builder,
            tier: BondTier::UpTo1Gbps,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 7,
        }
    }

    /// A real `Permission`, serialized the way the program writes one.
    fn permission_account(
        program_id: Pubkey,
        status: PermissionStatus,
        permissions: u128,
    ) -> Account {
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status,
            user_payer: Pubkey::new_unique(),
            permissions,
        };
        Account {
            lamports: 1,
            data: borsh::to_vec(&permission).unwrap(),
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        }
    }

    fn usable(program_id: Pubkey) -> Account {
        permission_account(
            program_id,
            PermissionStatus::Activated,
            permission_flags::BOND_ORACLE,
        )
    }

    //
    // The preflight, against accounts directly. No mocks, so every reason is covered.
    //

    #[test]
    fn test_an_activated_permission_with_the_flag_passes() {
        let program_id = Pubkey::new_unique();
        assert_eq!(
            check_bond_oracle(Some(&usable(program_id)), &program_id),
            Ok(())
        );
    }

    #[test]
    fn test_no_account_or_a_foreign_owner_holds_nothing() {
        let program_id = Pubkey::new_unique();
        assert_eq!(
            check_bond_oracle(None, &program_id),
            Err(BondOraclePreflight::NoPermissionAccount)
        );
        assert_eq!(
            check_bond_oracle(Some(&usable(Pubkey::new_unique())), &program_id),
            Err(BondOraclePreflight::NoPermissionAccount)
        );
    }

    #[test]
    fn test_an_account_that_is_not_a_permission_is_refused() {
        let program_id = Pubkey::new_unique();
        let junk = Account {
            lamports: 1,
            data: vec![],
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        };
        assert_eq!(
            check_bond_oracle(Some(&junk), &program_id),
            Err(BondOraclePreflight::NotAPermission)
        );
    }

    /// A suspended Permission is what revoking the relayer's key looks like, and `authorize`
    /// rejects it, so the preflight has to as well.
    #[test]
    fn test_a_suspended_or_unactivated_permission_is_refused() {
        let program_id = Pubkey::new_unique();
        for status in [PermissionStatus::Suspended, PermissionStatus::None] {
            let account = permission_account(program_id, status, permission_flags::BOND_ORACLE);
            assert_eq!(
                check_bond_oracle(Some(&account), &program_id),
                Err(BondOraclePreflight::NotActivated(status))
            );
        }
    }

    /// Holding some other role is not holding this one.
    #[test]
    fn test_a_permission_without_the_flag_is_refused() {
        let program_id = Pubkey::new_unique();
        let account = permission_account(
            program_id,
            PermissionStatus::Activated,
            permission_flags::FOUNDATION | permission_flags::HEALTH_ORACLE,
        );
        assert_eq!(
            check_bond_oracle(Some(&account), &program_id),
            Err(BondOraclePreflight::MissingBondOracle)
        );
    }

    //
    // The command, proving the preflight is wired in and the account is appended.
    //

    #[test]
    fn test_write_bond_mirror_appends_the_permission_account() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let bond_ref = Pubkey::new_unique();
        let (permission_pubkey, _) = get_permission_pda(&program_id, &payer);

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pubkey]))
            .returning(move |_| Ok(vec![Some(usable(program_id))]));
        client
            .expect_send_transaction()
            .withf(move |ix| {
                ix.accounts
                    .last()
                    .is_some_and(|a| a.pubkey == permission_pubkey && !a.is_writable)
            })
            .returning(|_| Ok(Signature::new_unique()));

        let (_, mirror) = command(bond_ref, Pubkey::new_unique())
            .execute(&client)
            .expect("a BOND_ORACLE holder can write");
        assert_eq!(mirror, get_bond_mirror_pda(&program_id, &bond_ref).0);
    }

    /// A caller the program would reject never gets a transaction sent on its behalf.
    #[test]
    fn test_an_unusable_permission_sends_nothing() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let (permission_pubkey, _) = get_permission_pda(&program_id, &client.get_payer());

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pubkey]))
            .returning(move |_| {
                Ok(vec![Some(permission_account(
                    program_id,
                    PermissionStatus::Suspended,
                    permission_flags::BOND_ORACLE,
                ))])
            });
        client.expect_send_transaction().never();

        // The assertion that matters is mockall's: `send_transaction` was set to `.never()`, so
        // this failing before reaching it is the whole point. The report itself is not inspected,
        // because which reason fired is covered exhaustively against accounts above.
        let _ = command(Pubkey::new_unique(), Pubkey::new_unique())
            .execute(&client)
            .expect_err("a suspended Permission cannot write");
    }
}
