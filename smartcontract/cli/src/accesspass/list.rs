use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
};
use doublezero_serviceability::state::accesspass::{AccessPassStatus, AccessPassType};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug, Default)]
pub struct ListAccessPassCliCommand {
    /// List prepaid access passes
    #[arg(long, default_value_t = false)]
    pub prepaid: bool,
    /// List Solana validator access passes
    #[arg(long, default_value_t = false)]
    pub solana_validator: bool,
    /// Solana identity public key
    #[arg(long)]
    pub solana_identity: Option<Pubkey>,
    /// Client IP address
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// User payer public key
    #[arg(long)]
    pub user_payer: Option<Pubkey>,

    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct AccessPassDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub accesspass_type: String,
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
    pub multicast: String,
    pub last_access_epoch: String,
    pub remaining_epoch: String,
    pub flags: String,
    pub connections: u16,
    pub status: AccessPassStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let epoch = client.get_epoch()?;

        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;

        let binding = client.list_accesspass(ListAccessPassCommand)?;
        let mut access_passes = binding.iter().collect::<Vec<_>>();

        // Filter access passes by type
        if self.prepaid {
            access_passes
                .retain(|(_, access_pass)| access_pass.accesspass_type == AccessPassType::Prepaid);
        }
        // Filter access passes by Solana validator
        if self.solana_validator {
            access_passes.retain(|(_, access_pass)| {
                matches!(
                    access_pass.accesspass_type,
                    AccessPassType::SolanaValidator(_)
                )
            });
        }
        // Filter access passes by Solana identity
        if let Some(solana_identity) = self.solana_identity {
            access_passes.retain(|(_, access_pass)| {
                access_pass.accesspass_type == AccessPassType::SolanaValidator(solana_identity)
            });
        }
        // Filter access passes by client IP
        if let Some(client_ip) = self.client_ip {
            access_passes.retain(|(_, access_pass)| access_pass.client_ip == client_ip);
        }
        // Filter access passes by user payer
        if let Some(user_payer) = self.user_payer {
            access_passes.retain(|(_, access_pass)| access_pass.user_payer == user_payer);
        }

        let mut access_pass_displays: Vec<AccessPassDisplay> = access_passes
            .into_iter()
            .map(|(pubkey, access_pass)| AccessPassDisplay {
                account: *pubkey,
                accesspass_type: access_pass.accesspass_type.to_string(),
                client_ip: access_pass.client_ip,
                user_payer: access_pass.user_payer,
                multicast: {
                    let mut list = vec![];
                    for mg_pub in &access_pass.mgroup_pub_allowlist {
                        if let Some(mg) = mgroups.get(mg_pub) {
                            list.push(format!("P:{}", mg.code));
                        } else {
                            list.push(format!("P:{mg_pub}"));
                        }
                    }
                    for mg_sub in &access_pass.mgroup_sub_allowlist {
                        if let Some(mg) = mgroups.get(mg_sub) {
                            list.push(format!("S:{}", mg.code));
                        } else {
                            list.push(format!("S:{mg_sub}"));
                        }
                    }
                    list.join(", ")
                },

                last_access_epoch: if access_pass.last_access_epoch == u64::MAX {
                    "MAX".to_string()
                } else {
                    access_pass.last_access_epoch.to_string()
                },
                remaining_epoch: if access_pass.last_access_epoch == u64::MAX {
                    "MAX".to_string()
                } else {
                    access_pass
                        .last_access_epoch
                        .saturating_sub(epoch)
                        .to_string()
                },
                flags: access_pass.flags_string(),
                connections: access_pass.connection_count,
                status: access_pass.status,
                owner: access_pass.owner,
            })
            .collect();

        access_pass_displays.sort_by(|a, b| a.client_ip.cmp(&b.client_ip));

        let res = if self.json {
            serde_json::to_string_pretty(&access_pass_displays)?
        } else if self.json_compact {
            serde_json::to_string(&access_pass_displays)?
        } else {
            Table::new(access_pass_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{accesspass::list::ListAccessPassCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType, IS_DYNAMIC,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_cli_accesspass_list() {
        let mut client = create_test_client();

        let mgroup_pubkey = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        let mgroup = doublezero_sdk::MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            code: "test".to_string(),
            publisher_count: 5,
            subscriber_count: 10,
        };

        let access1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let access1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: Ipv4Addr::new(1, 2, 3, 4),
            accesspass_type: AccessPassType::Prepaid,
            user_payer: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            last_access_epoch: 123,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };

        let access2_pubkey = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        let access2 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: Ipv4Addr::UNSPECIFIED,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB",
            )),
            user_payer: Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM"),
            last_access_epoch: 123,
            owner: Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM"),
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: IS_DYNAMIC,
        };

        let access3_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let access3 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: Ipv4Addr::new(2, 3, 4, 5),
            accesspass_type: AccessPassType::Prepaid,
            user_payer: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            last_access_epoch: 123,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };

        client.expect_get_epoch().returning(move || Ok(123));
        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = HashMap::new();
            mgroups.insert(mgroup_pubkey, mgroup.clone());
            Ok(mgroups)
        });
        client.expect_list_accesspass().returning(move |_| {
            let mut access_passes = HashMap::new();
            access_passes.insert(access1_pubkey, access1.clone());
            access_passes.insert(access2_pubkey, access2.clone());
            access_passes.insert(access3_pubkey, access3.clone());
            Ok(access_passes)
        });

        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            prepaid: false,
            client_ip: None,
            user_payer: None,
            solana_validator: false,
            solana_identity: None,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type                                             | client_ip | user_payer                                | multicast | last_access_epoch | remaining_epoch | flags   | connections | status    | owner                                     \n 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  | solana_validator: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | 0.0.0.0   | 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  | S:test    | 123               | 113             | dynamic | 0           | connected | 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid                                                     | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | P:test    | 123               | 113             |         | 0           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | prepaid                                                     | 2.3.4.5   | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | P:test    | 123               | 113             |         | 0           | connected | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            json: false,
            json_compact: true,
            client_ip: None,
            user_payer: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"accesspass_type\":\"solana_validator: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"client_ip\":\"0.0.0.0\",\"user_payer\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"multicast\":\"S:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"dynamic\",\"connections\":0,\"status\":\"Connected\",\"owner\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\"},{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"accesspass_type\":\"prepaid\",\"client_ip\":\"1.2.3.4\",\"user_payer\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"multicast\":\"P:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"\",\"connections\":0,\"status\":\"Connected\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"},{\"account\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"accesspass_type\":\"prepaid\",\"client_ip\":\"2.3.4.5\",\"user_payer\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"multicast\":\"P:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"\",\"connections\":0,\"status\":\"Connected\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\"}]\n");

        // Test filtering by client IP
        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            client_ip: Some(Ipv4Addr::new(1, 2, 3, 4)),
            ..Default::default()
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type | client_ip | user_payer                                | multicast | last_access_epoch | remaining_epoch | flags | connections | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid         | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | P:test    | 123               | 113             |       | 0           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        // Test filtering by user payer
        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            user_payer: Some(Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB",
            )),
            ..Default::default()
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type | client_ip | user_payer                                | multicast | last_access_epoch | remaining_epoch | flags | connections | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid         | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | P:test    | 123               | 113             |       | 0           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");
    }
}
