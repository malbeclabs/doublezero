use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::accesspass::set_feeds::SetAccessPassFeedsArgs, state::accesspass::FeedSeat,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

/// Provision feed seats (SKUs) onto an EdgeSeat access pass.
///
/// On-chain account layout (see `process_set_access_pass_feeds`):
///   `[accesspass, globalstate, payer, system, feed_0 .. feed_{N-1}, (optional permission)]`
///
/// `DoubleZeroClient::execute_transaction` appends `[payer, system]` after the base accounts
/// supplied here, so the base list is `[accesspass, globalstate]` followed by one writable `Feed`
/// account per seat, in the same order as `feeds`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassFeedsCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub feeds: Vec<FeedSeat>,
}

impl SetAccessPassFeedsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        let mut accounts = vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ];

        // One writable Feed account per seat, in the same order as `feeds`.
        for seat in &self.feeds {
            accounts.push(AccountMeta::new(seat.feed_key, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
                feeds: self.feeds.clone(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_feeds::SetAccessPassFeedsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda},
        processors::accesspass::set_feeds::SetAccessPassFeedsArgs,
        state::accesspass::FeedSeat,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_feeds_command() {
        let mut client = create_test_client();

        let client_ip = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);

        let seats = vec![FeedSeat {
            feed_key,
            max_users: 5,
            current_users: 0,
        }];

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPassFeeds(
                    SetAccessPassFeedsArgs {
                        client_ip,
                        user_payer: payer,
                        feeds: seats.clone(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(feed_key, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAccessPassFeedsCommand {
            client_ip,
            user_payer: payer,
            feeds: seats,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
