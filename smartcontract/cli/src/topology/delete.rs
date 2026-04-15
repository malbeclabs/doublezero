use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{
    commands::{link::list::ListLinkCommand, topology::delete::DeleteTopologyCommand},
    get_topology_pda,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteTopologyCliCommand {
    /// Name of the topology to delete
    #[arg(long)]
    pub name: String,
}

impl DeleteTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let name = self.name.to_lowercase();

        // Guard: check if any links still reference this topology
        let program_id = client.get_program_id();
        let topology_pda = get_topology_pda(&program_id, &name).0;
        let links = client.list_link(ListLinkCommand)?;
        let referencing_count = links
            .values()
            .filter(|link| link.link_topologies.contains(&topology_pda))
            .count();
        if referencing_count > 0 {
            return Err(eyre::eyre!(
                "Cannot delete topology '{}': {} link(s) still reference it. Run 'doublezero link topology clear --name {}' first.",
                name,
                referencing_count,
                name,
            ));
        }

        client.delete_topology(DeleteTopologyCommand { name: name.clone() })?;
        writeln!(out, "Deleted topology '{}' successfully.", name)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{doublezerocommand::MockCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::topology::delete::DeleteTopologyCommand, get_topology_pda, Link, LinkLinkType,
        LinkStatus,
    };
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        link::{LinkDesiredStatus, LinkHealth},
    };
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, io::Cursor};

    #[test]
    fn test_delete_topology_execute_success() {
        let mut mock = MockCliCommand::new();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_get_program_id()
            .returning(|| Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah"));
        mock.expect_list_link().returning(|_| Ok(HashMap::new()));
        mock.expect_delete_topology()
            .with(eq(DeleteTopologyCommand {
                name: "unicast-default".to_string(),
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let cmd = DeleteTopologyCliCommand {
            name: "unicast-default".to_string(),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Deleted topology 'unicast-default' successfully."));
    }

    #[test]
    fn test_delete_topology_blocked_by_referencing_links() {
        let mut client = create_test_client();

        client.expect_check_requirements().returning(|_| Ok(()));

        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let topology_pda = get_topology_pda(&program_id, "unicast-default").0;

        let link = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 2,
            code: "link1".to_string(),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 4500,
            delay_ns: 0,
            jitter_ns: 0,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.0/30".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: Pubkey::new_unique(),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
            link_topologies: vec![topology_pda],
            link_flags: 0,
        };

        client.expect_list_link().returning(move |_| {
            let mut links = HashMap::new();
            links.insert(Pubkey::new_unique(), link.clone());
            Ok(links)
        });

        let cmd = DeleteTopologyCliCommand {
            name: "unicast-default".to_string(),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&client, &mut out);
        assert!(result.is_err());
        let err = result.unwrap_err().to_string();
        assert!(err.contains("Cannot delete topology 'unicast-default'"));
        assert!(err.contains("1 link(s) still reference it"));
        assert!(err.contains("doublezero link topology clear --name unicast-default"));
    }
}
