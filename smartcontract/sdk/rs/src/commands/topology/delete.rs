use crate::DoubleZeroClient;
use doublezero_serviceability::processors::topology::delete::TopologyDeleteArgs;
use doublezero_serviceability_instruction::topology::delete_topology;
use solana_sdk::signature::Signature;

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteTopologyCommand {
    pub name: String,
}

impl DeleteTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(delete_topology(
            &client.get_program_id(),
            &client.get_payer(),
            TopologyDeleteArgs {
                name: self.name.clone(),
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::delete::DeleteTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::topology::delete::TopologyDeleteArgs;
    use doublezero_serviceability_instruction::topology::delete_topology;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_topology_delete_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let expected = delete_topology(
            &program_id,
            &payer,
            TopologyDeleteArgs {
                name: "unicast-default".to_string(),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteTopologyCommand {
            name: "unicast-default".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
