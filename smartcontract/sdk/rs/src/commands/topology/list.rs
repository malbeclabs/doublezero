use crate::DoubleZeroClient;
use doublezero_serviceability::{
    error::DoubleZeroError,
    state::{accountdata::AccountData, accounttype::AccountType, topology::TopologyInfo},
};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct ListTopologyCommand;

impl ListTopologyCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<HashMap<Pubkey, TopologyInfo>> {
        client
            .gets(AccountType::Topology)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Topology(topology) => Ok((k, topology)),
                _ => Err(DoubleZeroError::InvalidAccountType.into()),
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::topology::list::ListTopologyCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        topology::{TopologyConstraint, TopologyInfo},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_topology_list_command() {
        let mut client = create_test_client();

        let topology1_pubkey = Pubkey::new_unique();
        let topology1 = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            name: "unicast-default".to_string(),
            admin_group_bit: 0,
            flex_algo_number: 128,
            constraint: TopologyConstraint::IncludeAny,
        };

        let topology2_pubkey = Pubkey::new_unique();
        let topology2 = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 2,
            name: "exclude-test".to_string(),
            admin_group_bit: 2,
            flex_algo_number: 130,
            constraint: TopologyConstraint::Exclude,
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Topology))
            .returning(move |_| {
                let mut topologies = HashMap::new();
                topologies.insert(topology1_pubkey, AccountData::Topology(topology1.clone()));
                topologies.insert(topology2_pubkey, AccountData::Topology(topology2.clone()));
                Ok(topologies)
            });

        let res = ListTopologyCommand.execute(&client);

        assert!(res.is_ok());
        let list = res.unwrap();
        assert_eq!(list.len(), 2);
        assert!(list.contains_key(&topology1_pubkey));
        assert!(list.contains_key(&topology2_pubkey));
    }
}
