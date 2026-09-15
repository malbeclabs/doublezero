use crate::DoubleZeroClient;
use doublezero_serviceability_instruction::feed::finalize_feed_retirement;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// End a retirement once its notice has elapsed.
///
/// Permissionless, so no `Permission` account is appended: nothing about this call is checked
/// against an authority, and the clock decides whether it succeeds.
#[derive(Debug, PartialEq, Clone)]
pub struct FinalizeFeedRetirementCommand {
    pub pubkey: Pubkey,
}

impl FinalizeFeedRetirementCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let ix =
            finalize_feed_retirement(&client.get_program_id(), &client.get_payer(), &self.pubkey);

        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::finalize_retirement::FinalizeFeedRetirementCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability_instruction::feed::finalize_feed_retirement;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    /// Finalize is permissionless, so it appends no `Permission` account. The mock is set with no
    /// expectation for `get_multiple_accounts`, which is what a lookup here would use: if the
    /// command ever started appending one, this fails rather than silently asking the program for
    /// an account it does not read.
    #[test]
    fn test_commands_feed_finalize_retirement_sends_no_permission_account() {
        let mut client = create_test_client();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let expected =
            finalize_feed_retirement(&client.get_program_id(), &client.get_payer(), &feed_pk);
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = FinalizeFeedRetirementCommand { pubkey: feed_pk }.execute(&client);
        assert_eq!(res.unwrap(), signature);
    }
}
