use crate::DoubleZeroClient;
use doublezero_serviceability::processors::multicastgroup::suspend::MulticastGroupSuspendArgs;
use doublezero_serviceability_instruction::multicastgroup::suspend_multicast_group;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendMulticastGroupCommand {
    pub pubkey: Pubkey,
}

impl SuspendMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(suspend_multicast_group(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            MulticastGroupSuspendArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::suspend::SuspendMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_location_pda, processors::multicastgroup::suspend::MulticastGroupSuspendArgs,
    };
    use doublezero_serviceability_instruction::multicastgroup::suspend_multicast_group;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_location_suspend_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let expected = suspend_multicast_group(
            &program_id,
            &payer,
            &pda_pubkey,
            MulticastGroupSuspendArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SuspendMulticastGroupCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
