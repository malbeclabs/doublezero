use std::net::Ipv4Addr;

use doublezero_serviceability::processors::accesspass::set_flags::SetAccessPassFlagsArgs;
use doublezero_serviceability_instruction::accesspass::set_access_pass_flags;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

/// Surgically set or clear individual bits of an existing access pass's flags byte. Each field is
/// tri-state: `None` leaves the flag unchanged, `Some(v)` sets it to `v`.
///
/// Gated on `ACCESS_PASS_ADMIN`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassFlagsCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub allow_multiple_ip: Option<bool>,
    pub dzf_locked: Option<bool>,
}

impl SetAccessPassFlagsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_access_pass_flags(
            &client.get_program_id(),
            &client.get_payer(),
            self.client_ip,
            &self.user_payer,
            SetAccessPassFlagsArgs {
                allow_multiple_ip: self.allow_multiple_ip,
                dzf_locked: self.dzf_locked,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_flags::SetAccessPassFlagsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::processors::accesspass::set_flags::SetAccessPassFlagsArgs;
    use doublezero_serviceability_instruction::accesspass::set_access_pass_flags;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_flags_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let expected = set_access_pass_flags(
            &program_id,
            &payer,
            client_ip,
            &user_payer,
            SetAccessPassFlagsArgs {
                allow_multiple_ip: None,
                dzf_locked: Some(true),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAccessPassFlagsCommand {
            client_ip,
            user_payer,
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
