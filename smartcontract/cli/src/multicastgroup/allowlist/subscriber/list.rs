use crate::doublezerocommand::CliCommand;
use ::serde::Serialize;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand, multicastgroup::get::GetMulticastGroupCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListMulticastGroupSubAllowlistCliCommand {
    // Multicast group code or pubkey to list publisher allowlist for
    #[arg(long)]
    pub code: String,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct MulticastAllowlistDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub multicast_group: String,
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
}

impl ListMulticastGroupSubAllowlistCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (mgroup_pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.code.clone(),
        })?;

        let list_accesspass = client.list_accesspass(ListAccessPassCommand {})?;

        let mga_displays = list_accesspass
            .into_iter()
            .filter(|(_, accesspass)| accesspass.mgroup_sub_allowlist.contains(&mgroup_pubkey))
            .map(|(_, accesspass)| MulticastAllowlistDisplay {
                account: mgroup_pubkey,
                multicast_group: mgroup.code.clone(),
                client_ip: accesspass.client_ip,
                user_payer: accesspass.user_payer,
            })
            .collect::<Vec<_>>();

        let res = if self.json {
            serde_json::to_string_pretty(&mga_displays)?
        } else if self.json_compact {
            serde_json::to_string(&mga_displays)?
        } else {
            Table::new(mga_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        multicastgroup::allowlist::subscriber::list::ListMulticastGroupSubAllowlistCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            accesspass::list::ListAccessPassCommand, multicastgroup::get::GetMulticastGroupCommand,
        },
        AccountType, MulticastGroup,
    };
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_multicast_subscriber_allowlist_list() {
        let mut client = create_test_client();

        let mgroup_pubkey = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            code: "test".to_string(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            tenant_pk: Pubkey::new_unique(),
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            publisher_count: 5,
            subscriber_count: 10,
        };

        let accesspass1_pk = Pubkey::from_str_const("1111111ogCyDbaRMvkdsHB3qfdyFYaG1WtRUAfdh");
        let accesspass1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: [100, 0, 0, 1].into(),
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: 1234,
            user_payer: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8"),
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo7"),
            connection_count: 0,
            status: AccessPassStatus::Requested,
            flags: 0,
        };

        let accesspass2_pk = Pubkey::from_str_const("11111112D1oxKts8YPdTJRG5FzxTNpMtWmq8hkVx3");
        let accesspass2 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: [100, 0, 0, 1].into(),
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: 1234,
            user_payer: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo6"),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo5"),
            connection_count: 0,
            status: AccessPassStatus::Requested,
            flags: 0,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));
        client
            .expect_list_accesspass()
            .with(predicate::eq(ListAccessPassCommand {}))
            .returning(move |_| {
                let mut list: HashMap<Pubkey, AccessPass> = HashMap::new();
                list.insert(accesspass1_pk, accesspass1.clone());
                list.insert(accesspass2_pk, accesspass2.clone());
                Ok(list)
            });

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = ListMulticastGroupSubAllowlistCliCommand {
            code: "test".to_string(),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str," account                                  | multicast_group | client_ip | user_payer                                \n 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM | test            | 100.0.0.1 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo6 \n"
        );

        let mut output = Vec::new();
        let res = ListMulticastGroupSubAllowlistCliCommand {
            code: "test".to_string(),
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"[{\"account\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"multicast_group\":\"test\",\"client_ip\":\"100.0.0.1\",\"user_payer\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo6\"}]\n"
        );
    }
}
