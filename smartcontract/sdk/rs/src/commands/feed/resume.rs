use crate::DoubleZeroClient;
use doublezero_serviceability_instruction::feed::resume_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Put a halted feed back to publishing.
#[derive(Debug, PartialEq, Clone)]
pub struct ResumeFeedCommand {
    pub pubkey: Pubkey,
    /// The feed's bond mirror, required for a bonded feed and `None` for a catalog one.
    ///
    /// Resuming re-proves that the bond still covers the feed's rate, because a mirror can be
    /// corrected downward while a feed sits halted. The caller derives it from the feed's own
    /// `bond_ref` rather than choosing it.
    pub bond_mirror: Option<Pubkey>,
}

impl ResumeFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let ix = resume_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            self.bond_mirror.as_ref(),
        );

        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::resume::ResumeFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };

    use doublezero_serviceability_instruction::feed::resume_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    /// Resuming re-proves the bond still covers the rate, so it carries the mirror. Dropping the mirror here would reach the program as a bonded feed with no bond
    /// to read, which it refuses.
    #[test]
    fn test_commands_feed_resume_forwards_the_bond_mirror() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let mirror = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let expected = resume_feed(&program_id, &payer, &feed_pk, Some(&mirror));

        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = ResumeFeedCommand {
            pubkey: feed_pk,
            bond_mirror: Some(mirror),
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    /// A catalog feed sends no mirror, so the instruction carries one account fewer.
    #[test]
    fn test_commands_feed_resume_sends_no_bond_mirror_for_a_catalog_feed() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let expected = resume_feed(&program_id, &payer, &feed_pk, None);

        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = ResumeFeedCommand {
            pubkey: feed_pk,
            bond_mirror: None,
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }
}
