use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::list::ListLinkCommand},
    DoubleZeroClient,
};
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

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        // Look up any link that references this device + interface and pass it
        // so the onchain program can validate link status for Activated interfaces.
        if let Ok(links) = ListLinkCommand.execute(client) {
            if let Some((link_pubkey, _)) = links.iter().find(|(_, link)| {
                (link.side_a_pk == self.pubkey
                    && link.side_a_iface_name.eq_ignore_ascii_case(&self.name))
                    || (link.side_z_pk == self.pubkey
                        && link.side_z_iface_name.eq_ignore_ascii_case(&self.name))
            }) {
                accounts.push(AccountMeta::new(*link_pubkey, false));
            }
        }

        client.execute_transaction(
            DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
                name: self.name.clone(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_serviceability::{
        pda::get_globalstate_pda,
        state::{accountdata::AccountData, accounttype::AccountType, link::Link},
    };
    use mockall::predicate;
    use std::collections::HashMap;

    #[test]
    fn test_commands_device_interface_unlink_command_no_link() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();

        // ListLinkCommand returns no links
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Link))
            .returning(|_| Ok(HashMap::new()));

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

    #[test]
    fn test_commands_device_interface_unlink_command_with_link() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let device_pubkey = Pubkey::new_unique();
        let link_pubkey = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            side_a_pk: device_pubkey,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_pk: Pubkey::new_unique(),
            side_z_iface_name: "Ethernet1".to_string(),
            ..Default::default()
        };

        let link_clone = link.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Link))
            .returning(move |_| {
                Ok(HashMap::from([(
                    link_pubkey,
                    AccountData::Link(link_clone.clone()),
                )]))
            });

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
                    AccountMeta::new(link_pubkey, false),
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
