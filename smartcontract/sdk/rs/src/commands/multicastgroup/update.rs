use crate::DoubleZeroClient;
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::processors::multicastgroup::update::MulticastGroupUpdateArgs;
use doublezero_serviceability_instruction::multicastgroup::update_multicast_group;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub multicast_ip: Option<Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
    pub publisher_count: Option<u32>,
    pub subscriber_count: Option<u32>,
    pub owner: Option<Pubkey>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        // The builder decides whether the multicast_group_block resource extension is needed and
        // sets `use_onchain_allocation` from `multicast_ip.is_some()`.
        client.send_transaction(update_multicast_group(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            MulticastGroupUpdateArgs {
                code,
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
                publisher_count: self.publisher_count,
                subscriber_count: self.subscriber_count,
                use_onchain_allocation: self.multicast_ip.is_some(),
                owner: self.owner,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::update::UpdateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_location_pda, processors::multicastgroup::update::MulticastGroupUpdateArgs,
    };
    use doublezero_serviceability_instruction::multicastgroup::update_multicast_group;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_multicastgroup_update_with_multicast_ip() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let expected = update_multicast_group(
            &program_id,
            &payer,
            &pda_pubkey,
            MulticastGroupUpdateArgs {
                code: Some("test_group".to_string()),
                multicast_ip: Some("239.0.0.1".parse().unwrap()),
                max_bandwidth: Some(1000),
                publisher_count: Some(10),
                subscriber_count: Some(100),
                use_onchain_allocation: true,
                owner: None,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let update_command = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: Some("test_group".to_string()),
            multicast_ip: Some("239.0.0.1".parse().unwrap()),
            max_bandwidth: Some(1000),
            publisher_count: Some(10),
            subscriber_count: Some(100),
            owner: None,
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

    #[test]
    fn test_commands_multicastgroup_update_without_multicast_ip() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        let expected = update_multicast_group(
            &program_id,
            &payer,
            &pda_pubkey,
            MulticastGroupUpdateArgs {
                code: None,
                multicast_ip: None,
                max_bandwidth: Some(2000),
                publisher_count: None,
                subscriber_count: None,
                use_onchain_allocation: false,
                owner: None,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: None,
            multicast_ip: None,
            max_bandwidth: Some(2000),
            publisher_count: None,
            subscriber_count: None,
            owner: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
