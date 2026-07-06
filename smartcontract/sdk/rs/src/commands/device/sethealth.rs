use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::sethealth::DeviceSetHealthArgs,
    state::device::DeviceHealth,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceHealthCommand {
    pub pubkey: Pubkey,
    pub health: DeviceHealth,
}

impl SetDeviceHealthCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_authorized_transaction(
            DoubleZeroInstruction::SetDeviceHealth(DeviceSetHealthArgs {
                health: self.health,
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
    fn test_commands_device_set_health_command() {
        // create_test_client already mocks the GlobalState `get` that
        // GetGlobalStateCommand reads before building the instruction.
        let mut client = create_test_client();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();
        let health = DeviceHealth::ReadyForUsers;

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceHealth(
                    DeviceSetHealthArgs { health },
                )),
                // Instruction accounts: [device, globalstate].
                predicate::function(move |accounts: &Vec<AccountMeta>| {
                    accounts.len() == 2
                        && accounts[0].pubkey == device_pubkey
                        && accounts[1].pubkey == globalstate_pubkey
                }),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = SetDeviceHealthCommand {
            pubkey: device_pubkey,
            health,
        };

        let res = command.execute(&client);
        assert!(res.is_ok());
    }
}
