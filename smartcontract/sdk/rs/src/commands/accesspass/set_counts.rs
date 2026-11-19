use doublezero_serviceability::processors::accesspass::set_counts::SetAccessPassCountsArgs;
use doublezero_serviceability_instruction::accesspass::set_access_pass_counts;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

/// Replace the unicast and public multicast user counts of an existing access pass. The program
/// sets the connection count to the sum of these two counts and the private multicast count. Use
/// it after you count the users on the pass offchain.
///
/// The `expected_*` fields are the counts read from the pass when the users were counted. The
/// program refuses the write with `AccessPassCountsChanged` when the pass no longer holds them.
///
/// Gated on `ACCESS_PASS_ADMIN`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassCountsCommand {
    pub pubkey: Pubkey,
    pub expected_connection_count: u16,
    pub expected_unicast_user_count: u16,
    pub expected_public_multicast_user_count: u16,
    pub unicast_user_count: u16,
    pub public_multicast_user_count: u16,
    pub private_multicast_user_count: u16,
}

impl SetAccessPassCountsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_access_pass_counts(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            SetAccessPassCountsArgs {
                expected_connection_count: self.expected_connection_count,
                expected_unicast_user_count: self.expected_unicast_user_count,
                expected_public_multicast_user_count: self.expected_public_multicast_user_count,
                unicast_user_count: self.unicast_user_count,
                public_multicast_user_count: self.public_multicast_user_count,
                private_multicast_user_count: self.private_multicast_user_count,
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
                expected_connection_count: 2,
                expected_unicast_user_count: 0,
                expected_public_multicast_user_count: 0,
                unicast_user_count: 3,
                public_multicast_user_count: 1,
                private_multicast_user_count: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAccessPassCountsCommand {
            pubkey: accesspass,
            expected_connection_count: 2,
            expected_unicast_user_count: 0,
            expected_public_multicast_user_count: 0,
            unicast_user_count: 3,
            public_multicast_user_count: 1,
            private_multicast_user_count: 1,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
