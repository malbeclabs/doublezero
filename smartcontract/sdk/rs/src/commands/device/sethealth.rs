use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::device::sethealth::DeviceSetHealthArgs, state::device::DeviceHealth,
};
use doublezero_serviceability_instruction::device::set_device_health;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceHealthCommand {
    pub pubkey: Pubkey,
    pub health: DeviceHealth,
}

impl SetDeviceHealthCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_device_health(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            DeviceSetHealthArgs {
                health: self.health,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_device_set_health_command() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let device_pubkey = Pubkey::new_unique();
        let health = DeviceHealth::ReadyForUsers;

        let expected = set_device_health(
            &program_id,
            &payer,
            &device_pubkey,
            DeviceSetHealthArgs { health },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let command = SetDeviceHealthCommand {
            pubkey: device_pubkey,
            health,
        };

        let res = command.execute(&client);
        assert!(res.is_ok());
    }
}
