use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::update::MulticastGroupUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub multicast_ip: Option<Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                code,
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::update::UpdateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::update::MulticastGroupUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        code: Some("test_group".to_string()),
                        multicast_ip: Some("127.0.0.1".parse().unwrap()),
                        max_bandwidth: Some(1000),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: Some("test_group".to_string()),
            multicast_ip: Some("127.0.0.1".parse().unwrap()),
            max_bandwidth: Some(1000),
        };

        let update_invalid_command = UpdateMulticastGroupCommand {
            code: Some("test/group".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());

        let res = update_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
