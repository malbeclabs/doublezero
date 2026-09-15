use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
use doublezero_serviceability::processors::feed::activate::FeedActivateArgs;
use doublezero_serviceability_instruction::feed::activate_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateFeedCommand {
    pub pubkey: Pubkey,
    /// The feed's stake mirror, required for a staked feed and `None` for a catalog one.
    ///
    /// Activation re-reads the mirror, so a staked feed without it is refused rather than read as
    /// having no stake to check. The caller derives it from the feed's own `stake_ref` rather than
    /// choosing it.
    pub stake_mirror: Option<Pubkey>,
}

impl ActivateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let mut ix = activate_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            self.stake_mirror.as_ref(),
        );

        append_payer_permission_account(client, &mut ix)?;
        client.send_transaction(ix)
    }
}
