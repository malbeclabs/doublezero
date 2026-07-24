use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::accesspass::set_flags::SetAccessPassFlagsArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

/// Surgically set or clear individual bits of an existing access pass's flags byte. Each field is
/// tri-state: `None` leaves the flag unchanged, `Some(v)` sets it to `v`.
///
/// On-chain account layout (see `process_set_access_pass_flags`):
///   `[accesspass, globalstate, payer, system, permission]`
///
/// `DoubleZeroClient::execute_authorized_transaction` appends `[payer, system, permission]` after
/// the base accounts supplied here, so the base list is `[accesspass, globalstate]`. Gated on
/// `ACCESS_PASS_ADMIN`.
#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassFlagsCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub allow_multiple_ip: Option<bool>,
    pub dzf_locked: Option<bool>,
}

impl SetAccessPassFlagsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        client.execute_authorized_transaction(
            DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
                allow_multiple_ip: self.allow_multiple_ip,
                dzf_locked: self.dzf_locked,
            }),
            vec![
                AccountMeta::new(accesspass_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set_flags::SetAccessPassFlagsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda},
        processors::accesspass::set_flags::SetAccessPassFlagsArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_flags_command() {
        let mut client = create_test_client();

        let client_ip = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPassFlags(
                    SetAccessPassFlagsArgs {
                        allow_multiple_ip: None,
                        dzf_locked: Some(true),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAccessPassFlagsCommand {
            client_ip,
            user_payer: payer,
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
