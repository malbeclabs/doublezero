use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
use doublezero_serviceability_instruction::feed::resume_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Put a halted feed back to publishing.
#[derive(Debug, PartialEq, Clone)]
pub struct ResumeFeedCommand {
    pub pubkey: Pubkey,
    /// The feed's stake mirror, required for a staked feed and `None` for a catalog one.
    ///
    /// Resuming re-proves that the stake still covers the feed's rate, because a mirror can be
    /// corrected downward while a feed sits halted. The caller derives it from the feed's own
    /// `stake_ref` rather than choosing it.
    pub stake_mirror: Option<Pubkey>,
}

impl ResumeFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let mut ix = resume_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            self.stake_mirror.as_ref(),
        );

        append_payer_permission_account(client, &mut ix)?;
        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::resume::ResumeFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::pda::get_permission_pda;
    use doublezero_serviceability_instruction::feed::resume_feed;
    use mockall::predicate;
    use solana_sdk::{
        account::Account, instruction::AccountMeta, pubkey::Pubkey, signature::Signature,
    };

    /// Resuming re-proves the stake still covers the rate, so it carries the mirror. Dropping the mirror here would reach the program as a staked feed with no stake
    /// to read, which it refuses.
    #[test]
    fn test_commands_feed_resume_forwards_the_stake_mirror() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let mirror = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let mut expected = resume_feed(&program_id, &payer, &feed_pk, Some(&mirror));
        let (permission_pda, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda, false));

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pda]))
            .returning(move |_| Ok(vec![Some(Account::new(0, 0, &program_id))]));
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = ResumeFeedCommand {
            pubkey: feed_pk,
            stake_mirror: Some(mirror),
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    /// A catalog feed sends no mirror, so the instruction carries one account fewer.
    #[test]
    fn test_commands_feed_resume_sends_no_stake_mirror_for_a_catalog_feed() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let mut expected = resume_feed(&program_id, &payer, &feed_pk, None);
        let (permission_pda, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda, false));

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pda]))
            .returning(move |_| Ok(vec![Some(Account::new(0, 0, &program_id))]));
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = ResumeFeedCommand {
            pubkey: feed_pk,
            stake_mirror: None,
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }
}
