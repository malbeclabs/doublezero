use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
use doublezero_serviceability_instruction::feed::retire_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Start a feed's retirement notice. Its seat holders keep what they have and it sells no
/// more. The notice runs thirty days, or none at all for a feed still `Pending`, which has no
/// seat holders to warn.
#[derive(Debug, PartialEq, Clone)]
pub struct RetireFeedCommand {
    pub pubkey: Pubkey,
}

impl RetireFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let mut ix = retire_feed(&client.get_program_id(), &client.get_payer(), &self.pubkey);

        append_payer_permission_account(client, &mut ix)?;
        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::retire::RetireFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::pda::get_permission_pda;
    use doublezero_serviceability_instruction::feed::retire_feed;
    use mockall::predicate;
    use solana_sdk::{
        account::Account, instruction::AccountMeta, pubkey::Pubkey, signature::Signature,
    };

    /// Retiring is authorized, unlike finalizing. The `Permission` account goes last, after the accounts the instruction builder names,
    /// because that is where `authorize` reads it.
    #[test]
    fn test_commands_feed_retire_appends_the_payer_permission_account() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let mut expected = retire_feed(&program_id, &payer, &feed_pk);
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

        let res = RetireFeedCommand { pubkey: feed_pk }.execute(&client);
        assert_eq!(res.unwrap(), signature);
    }
}
