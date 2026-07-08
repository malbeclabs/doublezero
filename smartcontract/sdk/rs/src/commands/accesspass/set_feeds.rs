use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_accesspass_pda,
    processors::accesspass::set_feeds::{FeedSeatConfig, SetAccessPassFeedsArgs},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

/// One feed seat (SKU) to provision: the `Feed` account to reference and its per-feed cap.
#[derive(Debug, PartialEq, Clone)]
pub struct FeedSeatProvision {
    pub feed_key: Pubkey,
    pub max_users: u16,
}

/// Provision feed seats (SKUs) onto an EdgeSeat access pass.
///
/// On-chain account layout (see `process_set_access_pass_feeds`):
///   `[accesspass, globalstate, feed_0 .. feed_{N-1}, payer, system, permission]`
///
/// `DoubleZeroClient::execute_authorized_transaction` appends `[payer, system, permission]` after
/// the base accounts supplied here, so the base list is `[accesspass, globalstate]` followed by one
/// read-only `Feed` account per seat, in the same order as `feeds`. The provisioning actor is
/// authorized via its `ACCESS_PASS_ADMIN` Permission, so the authorized variant is required.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassFeedsCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub feeds: Vec<FeedSeatProvision>,
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

        // One read-only Feed account per seat, in the same order as `feeds` (feeds are read to bind
        // feed_key and confirm existence; they are not mutated).
        for seat in &self.feeds {
            accounts.push(AccountMeta::new_readonly(seat.feed_key, false));
        }

        client.execute_authorized_transaction(
            DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
                feeds: self
                    .feeds
                    .iter()
                    .map(|seat| FeedSeatConfig {
                        max_users: seat.max_users,
                    })
                    .collect(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_feeds::{FeedSeatProvision, SetAccessPassFeedsCommand},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda},
        processors::accesspass::set_feeds::{FeedSeatConfig, SetAccessPassFeedsArgs},
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

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPassFeeds(
                    SetAccessPassFeedsArgs {
                        client_ip,
                        user_payer: payer,
                        feeds: vec![FeedSeatConfig { max_users: 5 }],
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new_readonly(feed_key, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAccessPassFeedsCommand {
            client_ip,
            user_payer: payer,
            feeds: vec![FeedSeatProvision {
                feed_key,
                max_users: 5,
            }],
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
