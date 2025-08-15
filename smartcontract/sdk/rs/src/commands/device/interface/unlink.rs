use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::interface::DeviceInterfaceUnlinkArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UnlinkDeviceInterfaceCommand {
    pub pubkey: Pubkey,
    pub name: String,
}

impl UnlinkDeviceInterfaceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
                name: self.name.clone(),
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
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::pda::get_globalstate_pda;
    use mockall::predicate;

    #[test]
    fn test_commands_device_interface_unlink_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UnlinkDeviceInterface(
                    DeviceInterfaceUnlinkArgs {
                        name: "Ethernet0".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UnlinkDeviceInterfaceCommand {
            pubkey: device_pubkey,
            name: "Ethernet0".to_string(),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
