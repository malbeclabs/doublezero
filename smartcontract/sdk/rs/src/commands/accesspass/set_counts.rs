use doublezero_serviceability::processors::accesspass::set_counts::SetAccessPassCountsArgs;
use doublezero_serviceability_instruction::accesspass::set_access_pass_counts;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

/// Overwrite the unicast and multicast user counts of an existing access pass. The program sets
/// the connection count to their sum. Use it after counting the users on the pass offchain.
///
/// Gated on `ACCESS_PASS_ADMIN`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassCountsCommand {
    pub pubkey: Pubkey,
    pub unicast_user_count: u16,
    pub multicast_user_count: u16,
}

impl SetAccessPassCountsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_access_pass_counts(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            SetAccessPassCountsArgs {
                unicast_user_count: self.unicast_user_count,
                multicast_user_count: self.multicast_user_count,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_counts::SetAccessPassCountsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::processors::accesspass::set_counts::SetAccessPassCountsArgs;
    use doublezero_serviceability_instruction::accesspass::set_access_pass_counts;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_counts_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let accesspass = Pubkey::new_unique();

        let expected = set_access_pass_counts(
            &program_id,
            &payer,
            &accesspass,
            SetAccessPassCountsArgs {
                unicast_user_count: 3,
                multicast_user_count: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAccessPassCountsCommand {
            pubkey: accesspass,
            unicast_user_count: 3,
            multicast_user_count: 1,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
