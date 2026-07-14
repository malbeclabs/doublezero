use std::net::Ipv4Addr;

use doublezero_serviceability::processors::accesspass::set_feeds::{
    FeedSeatConfig, SetAccessPassFeedsArgs,
};
use doublezero_serviceability_instruction::accesspass::set_access_pass_feeds;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

/// One feed seat (SKU) to provision: the `Feed` account to reference and its per-feed billing
/// state (current cap, future cap, anniversary day, and the window/termination boundaries).
#[derive(Debug, PartialEq, Clone)]
pub struct FeedSeatProvision {
    pub feed_key: Pubkey,
    pub max_users: u8,
    pub max_future_users: u8,
    pub anniversary_day: u8,
    pub window_end: i64,
    pub terminates_at: i64,
}

/// Provision feed seats (SKUs) onto an EdgeSeat access pass.
///
/// On-chain account layout (see `process_set_access_pass_feeds`):
///   `[accesspass, globalstate, feed_0 .. feed_{N-1}, payer, system, permission]`
///
/// The instruction builder derives `[accesspass, globalstate]` and appends one read-only `Feed`
/// account per seat, in the same order as `feeds`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassFeedsCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub feeds: Vec<FeedSeatProvision>,
}

impl SetAccessPassFeedsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        // One read-only Feed account per seat, in the same order as `feeds`.
        let feed_keys: Vec<Pubkey> = self.feeds.iter().map(|seat| seat.feed_key).collect();

        client.send_transaction(set_access_pass_feeds(
            &client.get_program_id(),
            &client.get_payer(),
            &feed_keys,
            SetAccessPassFeedsArgs {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
                feeds: self
                    .feeds
                    .iter()
                    .map(|seat| FeedSeatConfig {
                        max_users: seat.max_users,
                        max_future_users: seat.max_future_users,
                        anniversary_day: seat.anniversary_day,
                        window_end: seat.window_end,
                        terminates_at: seat.terminates_at,
                    })
                    .collect(),
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_feeds::{FeedSeatProvision, SetAccessPassFeedsCommand},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::accesspass::set_feeds::{
        FeedSeatConfig, SetAccessPassFeedsArgs,
    };
    use doublezero_serviceability_instruction::accesspass::set_access_pass_feeds;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_feeds_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();

        let expected = set_access_pass_feeds(
            &program_id,
            &payer,
            &[feed_key],
            SetAccessPassFeedsArgs {
                client_ip,
                user_payer,
                feeds: vec![FeedSeatConfig {
                    max_users: 5,
                    max_future_users: 8,
                    anniversary_day: 15,
                    window_end: 1_800_000_000,
                    terminates_at: 1_900_000_000,
                }],
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAccessPassFeedsCommand {
            client_ip,
            user_payer,
            feeds: vec![FeedSeatProvision {
                feed_key,
                max_users: 5,
                max_future_users: 8,
                anniversary_day: 15,
                window_end: 1_800_000_000,
                terminates_at: 1_900_000_000,
            }],
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
