use doublezero_serviceability::{
    pda::{get_permission_pda, get_stake_mirror_pda},
    processors::stake_mirror::write::StakeMirrorWriteArgs,
    state::stake_mirror::StakeTier,
};
use doublezero_serviceability_instruction::stake_mirror::write_stake_mirror;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

/// Copy a builder's Solana stake onto the DZ ledger. The relayer's write.
#[derive(Debug, PartialEq, Clone)]
pub struct WriteStakeMirrorCommand {
    /// The `builder-stake` account on Solana being mirrored. The PDA seed.
    pub stake_ref: Pubkey,
    pub builder: Pubkey,
    pub tier: StakeTier,
    pub committed_rate_bits_per_sec: u64,
    /// The Solana slot these values were read at. The program refuses a write whose slot is not
    /// strictly newer than the stored one, so a caller must track what it last wrote.
    pub source_slot: u64,
}

impl WriteStakeMirrorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (mirror_pubkey, _) = get_stake_mirror_pda(&program_id, &self.stake_ref);

        let mut ix = write_stake_mirror(
            &program_id,
            &payer,
            StakeMirrorWriteArgs {
                stake_ref: self.stake_ref,
                builder: self.builder,
                tier: self.tier,
                committed_rate_bits_per_sec: self.committed_rate_bits_per_sec,
                source_slot: self.source_slot,
            },
        );

        // Not `append_payer_permission_account`, which appends the account only when it already
        // exists. That is right for an instruction with a legacy `GlobalState` fallback, and wrong
        // here: no legacy key satisfies `STAKE_ORACLE`, so a missing Permission account is always
        // fatal. Saying so costs one RPC round trip and saves reading `NotAllowed` out of a failed
        // simulation.
        let (permission_pubkey, _) = get_permission_pda(&program_id, &payer);
        client
            .get_multiple_accounts(vec![permission_pubkey])?
            .into_iter()
            .flatten()
            .next()
            .filter(|account| account.owner == program_id)
            .ok_or_else(|| {
                eyre::eyre!(
                    "{payer} has no Permission account at {permission_pubkey}. \
                     WriteStakeMirror needs STAKE_ORACLE, and no GlobalState key grants it, \
                     so a Permission account is the only way to hold it."
                )
            })?;
        ix.accounts
            .push(AccountMeta::new_readonly(permission_pubkey, false));

        client.send_transaction(ix).map(|sig| (sig, mirror_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::pda::get_permission_pda;
    use mockall::predicate;
    use solana_sdk::{account::Account, signature::Signature};

    use super::*;
    use crate::tests::utils::create_test_client;

    fn command(stake_ref: Pubkey, builder: Pubkey) -> WriteStakeMirrorCommand {
        WriteStakeMirrorCommand {
            stake_ref,
            builder,
            tier: StakeTier::UpTo1Gbps,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 7,
        }
    }

    fn permission_account(owner: Pubkey) -> Account {
        Account {
            lamports: 1,
            data: vec![],
            owner,
            executable: false,
            rent_epoch: 0,
        }
    }

    /// The happy path appends the caller's Permission account and returns the mirror address.
    #[test]
    fn test_write_stake_mirror_appends_the_permission_account() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let stake_ref = Pubkey::new_unique();
        let (permission_pubkey, _) = get_permission_pda(&program_id, &payer);

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pubkey]))
            .returning(move |_| Ok(vec![Some(permission_account(program_id))]));
        client
            .expect_send_transaction()
            .withf(move |ix| {
                ix.accounts
                    .last()
                    .is_some_and(|a| a.pubkey == permission_pubkey && !a.is_writable)
            })
            .returning(|_| Ok(Signature::new_unique()));

        let (_, mirror) = command(stake_ref, Pubkey::new_unique())
            .execute(&client)
            .expect("a STAKE_ORACLE holder can write");
        assert_eq!(mirror, get_stake_mirror_pda(&program_id, &stake_ref).0);
    }

    /// No Permission account means the caller cannot hold `STAKE_ORACLE`, and no `GlobalState` key
    /// grants it, so the command says so rather than sending a transaction that must fail.
    #[test]
    fn test_a_caller_without_a_permission_account_is_refused_locally() {
        let mut client = create_test_client();
        let (permission_pubkey, _) =
            get_permission_pda(&client.get_program_id(), &client.get_payer());

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pubkey]))
            .returning(|_| Ok(vec![None]));
        client.expect_send_transaction().never();

        let err = command(Pubkey::new_unique(), Pubkey::new_unique())
            .execute(&client)
            .expect_err("no Permission account, so no STAKE_ORACLE");
        assert!(err.to_string().contains("STAKE_ORACLE"), "{err}");
    }

    /// An account at the right address owned by someone else is not a Permission account.
    #[test]
    fn test_a_foreign_owned_account_is_not_a_permission() {
        let mut client = create_test_client();
        let (permission_pubkey, _) =
            get_permission_pda(&client.get_program_id(), &client.get_payer());

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pubkey]))
            .returning(|_| Ok(vec![Some(permission_account(Pubkey::new_unique()))]));
        client.expect_send_transaction().never();

        assert!(command(Pubkey::new_unique(), Pubkey::new_unique())
            .execute(&client)
            .is_err());
    }
}
