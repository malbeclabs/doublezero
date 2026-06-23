use crate::{doublezerocommand::CliCommand, user::list::narrow_groups};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_program_common::serializer;
use doublezero_sdk::{
    commands::{
        accesspass::list::ListAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
        tenant::list::ListTenantCommand,
    },
    MulticastGroup,
};
use doublezero_serviceability::state::accesspass::{AccessPassStatus, AccessPassType};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr};
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
    /// List EdgeSeat access passes
    #[arg(long, default_value_t = false)]
    pub edge_seat: bool,
    /// Client IP address
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// User payer public key
    #[arg(long)]
    pub user_payer: Option<Pubkey>,
    /// Tenant code
    #[arg(long)]
    pub tenant: Option<String>,

    /// Access passes that allowlist the multicast group publisher with the specified code
    #[arg(long)]
    pub multicast_group_publisher: Option<String>,
    /// Access passes that do not allowlist the multicast group publisher with the specified code
    #[arg(long)]
    pub not_multicast_group_publisher: Option<String>,
    /// Access passes that allowlist the multicast group subscriber with the specified code
    #[arg(long)]
    pub multicast_group_subscriber: Option<String>,
    /// Access passes that do not allowlist the multicast group subscriber with the specified code
    #[arg(long)]
    pub not_multicast_group_subscriber: Option<String>,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
    /// Narrow table output: shortens pubkeys, abbreviates accesspass_type, shows
    /// the first multicast group per role (`+N` for the rest), maps the default
    /// tenant pubkey to `empty`, and shortens the epoch/connections headers.
    #[arg(long, default_value_t = false)]
    pub narrow: bool,
}

#[derive(Tabled, Serialize)]
pub struct AccessPassDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub accesspass_type: String,
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
    pub tenant: String,
    pub multicast: String,
    pub last_access_epoch: String,
    pub remaining_epoch: String,
    pub flags: String,
    pub connections: u16,
    pub unicast_users: String,
    pub multicast_users: String,
    pub status: AccessPassStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    #[tabled(skip)]
    #[serde(skip)]
    pub accesspass_type_value: AccessPassType,
    #[tabled(skip)]
    #[serde(skip)]
    pub mgroup_pub_allowlist: Vec<Pubkey>,
    #[tabled(skip)]
    #[serde(skip)]
    pub mgroup_sub_allowlist: Vec<Pubkey>,
}

/// Narrow variant of [`AccessPassDisplay`] for terminals: shortens every
/// pubkey, abbreviates the multicast and accesspass_type columns, maps the
/// blank tenant pubkey to `empty`, and shortens the wider headers.
#[derive(Tabled)]
pub struct AccessPassDisplayNarrow {
    #[tabled(display = "crate::util::display_pubkey_short")]
    pub account: Pubkey,
    pub accesspass_type: String,
    pub client_ip: Ipv4Addr,
    #[tabled(display = "crate::util::display_pubkey_short")]
    pub user_payer: Pubkey,
    pub tenant: String,
    pub multicast: String,
    #[tabled(rename = "lst_epch")]
    pub last_access_epoch: String,
    #[tabled(rename = "rem_epch")]
    pub remaining_epoch: String,
    pub flags: String,
    #[tabled(rename = "conns")]
    pub connections: u16,
    pub unicast_users: String,
    pub multicast_users: String,
    pub status: AccessPassStatus,
    #[tabled(display = "crate::util::display_pubkey_short")]
    pub owner: Pubkey,
}

/// Abbreviate the accesspass type for narrow output, shortening every embedded
/// key. The match is exhaustive on purpose: a new payload-carrying variant must
/// be handled here rather than silently rendering full-width.
fn accesspass_type_short(t: &AccessPassType) -> String {
    match t {
        AccessPassType::SolanaValidator(pk) => {
            format!(
                "solana_validator: {}",
                crate::util::display_pubkey_short(pk)
            )
        }
        AccessPassType::SolanaRPC(pk) => {
            format!("solana_rpc: {}", crate::util::display_pubkey_short(pk))
        }
        AccessPassType::Others(type_name, key) => {
            format!("others: {type_name} ({})", shorten_str(key))
        }
        AccessPassType::Prepaid | AccessPassType::EdgeSeat => t.to_string(),
    }
}

/// Truncate an arbitrary string to a copyable leading-10-char prefix + `..`
/// (matching the pubkey abbreviation length) for narrow output. char-based, so
/// it never splits a multibyte boundary on a non-ASCII `Others` key.
fn shorten_str(s: &str) -> String {
    if s.chars().count() > 12 {
        let prefix: String = s.chars().take(10).collect();
        format!("{prefix}..")
    } else {
        s.to_string()
    }
}

impl AccessPassDisplayNarrow {
    fn from_display(d: &AccessPassDisplay, mgroups: &HashMap<Pubkey, MulticastGroup>) -> Self {
        Self {
            account: d.account,
            accesspass_type: accesspass_type_short(&d.accesspass_type_value),
            client_ip: d.client_ip,
            user_payer: d.user_payer,
            // A literal default (all-ones) tenant pubkey reads as blank; show
            // "empty". Non-default tenants stringify differently and are
            // unaffected; the substring can't occur inside a real code/pubkey.
            tenant: d.tenant.replace(&Pubkey::default().to_string(), "empty"),
            multicast: narrow_groups(&d.mgroup_pub_allowlist, &d.mgroup_sub_allowlist, mgroups),
            last_access_epoch: d.last_access_epoch.clone(),
            remaining_epoch: d.remaining_epoch.clone(),
            flags: d.flags.clone(),
            connections: d.connections,
            unicast_users: d.unicast_users.clone(),
            multicast_users: d.multicast_users.clone(),
            status: d.status,
            owner: d.owner,
        }
    }
}

impl ListAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let epoch = client.get_epoch()?;

        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let tenants = client.list_tenant(ListTenantCommand {})?;

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
        // Filter access passes by EdgeSeat type
        if self.edge_seat {
            access_passes.retain(|(_, access_pass)| {
                matches!(access_pass.accesspass_type, AccessPassType::EdgeSeat)
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
        // Filter access passes by tenant code
        if let Some(tenant_code) = self.tenant {
            let search_tenant_pk = tenants
                .iter()
                .find_map(|(pk, tenant)| {
                    if tenant.code == tenant_code {
                        Some(*pk)
                    } else {
                        None
                    }
                })
                .unwrap_or_default();
            access_passes.retain(|(_, access_pass)| {
                access_pass
                    .tenant_allowlist
                    .iter()
                    .any(|tenant_pk| tenant_pk == &search_tenant_pk)
            });
        }

        if let Some(multicast_publisher) = self.multicast_group_publisher {
            access_passes.retain(|(_, access_pass)| {
                access_pass.mgroup_pub_allowlist.iter().any(|mg_pub| {
                    if let Some(mg) = mgroups.get(mg_pub) {
                        mg.code == multicast_publisher
                    } else {
                        false
                    }
                })
            });
        }

        if let Some(multicast_group_subscriber) = self.multicast_group_subscriber {
            access_passes.retain(|(_, access_pass)| {
                access_pass.mgroup_sub_allowlist.iter().any(|mg_sub| {
                    if let Some(mg) = mgroups.get(mg_sub) {
                        mg.code == multicast_group_subscriber
                    } else {
                        false
                    }
                })
            });
        }

        if let Some(not_multicast_group_publisher) = self.not_multicast_group_publisher {
            access_passes.retain(|(_, access_pass)| {
                !access_pass.mgroup_pub_allowlist.iter().any(|mg_pub| {
                    if let Some(mg) = mgroups.get(mg_pub) {
                        mg.code == not_multicast_group_publisher
                    } else {
                        false
                    }
                })
            });
        }

        if let Some(not_multicast_group_subscriber) = self.not_multicast_group_subscriber {
            access_passes.retain(|(_, access_pass)| {
                !access_pass.mgroup_sub_allowlist.iter().any(|mg_sub| {
                    if let Some(mg) = mgroups.get(mg_sub) {
                        mg.code == not_multicast_group_subscriber
                    } else {
                        false
                    }
                })
            });
        }

        let mut access_pass_displays: Vec<AccessPassDisplay> = access_passes
            .into_iter()
            .map(|(pubkey, access_pass)| AccessPassDisplay {
                account: *pubkey,
                accesspass_type: access_pass.accesspass_type.to_string(),
                client_ip: access_pass.client_ip,
                user_payer: access_pass.user_payer,
                tenant: {
                    let mut list = vec![];
                    for tenant_pk in &access_pass.tenant_allowlist {
                        if let Some(t) = tenants.get(tenant_pk) {
                            list.push(t.code.clone());
                        } else {
                            list.push(tenant_pk.to_string());
                        }
                    }
                    list.join(", ")
                },
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
                unicast_users: format!(
                    "{} / {}",
                    access_pass.unicast_user_count, access_pass.max_unicast_users
                ),
                multicast_users: format!(
                    "{} / {}",
                    access_pass.multicast_user_count, access_pass.max_multicast_users
                ),
                status: access_pass.status,
                owner: access_pass.owner,
                accesspass_type_value: access_pass.accesspass_type.clone(),
                mgroup_pub_allowlist: access_pass.mgroup_pub_allowlist.clone(),
                mgroup_sub_allowlist: access_pass.mgroup_sub_allowlist.clone(),
            })
            .collect();

        access_pass_displays.sort_by(|a, b| a.client_ip.cmp(&b.client_ip));

        let res = if self.json {
            serde_json::to_string_pretty(&access_pass_displays)?
        } else if self.json_compact {
            serde_json::to_string(&access_pass_displays)?
        } else if self.narrow {
            let narrow: Vec<AccessPassDisplayNarrow> = access_pass_displays
                .iter()
                .map(|d| AccessPassDisplayNarrow::from_display(d, &mgroups))
                .collect();
            Table::new(narrow)
                .with(Style::psql().remove_horizontals())
                .to_string()
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
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
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
            max_bandwidth: 1_000_000_000,
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
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
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
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
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
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client.expect_get_epoch().returning(move || Ok(123));
        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = HashMap::new();
            mgroups.insert(mgroup_pubkey, mgroup.clone());
            Ok(mgroups)
        });
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client.expect_list_accesspass().returning(move |_| {
            let mut access_passes = HashMap::new();
            access_passes.insert(access1_pubkey, access1.clone());
            access_passes.insert(access2_pubkey, access2.clone());
            access_passes.insert(access3_pubkey, access3.clone());
            Ok(access_passes)
        });

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                prepaid: false,
                client_ip: None,
                user_payer: None,
                tenant: None,
                solana_validator: false,
                solana_identity: None,
                edge_seat: false,
                multicast_group_publisher: None,
                multicast_group_subscriber: None,
                not_multicast_group_publisher: None,
                not_multicast_group_subscriber: None,
                json: false,
                json_compact: false,
                narrow: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type                                             | client_ip | user_payer                                | tenant | multicast | last_access_epoch | remaining_epoch | flags | connections | unicast_users | multicast_users | status    | owner                                     \n 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  | solana_validator: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | 0.0.0.0   | 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  |        | S:test    | 123               | 113             |       | 0           | 0 / 1         | 0 / 1           | connected | 1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM  \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid                                                     | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB |        | P:test    | 123               | 113             |       | 0           | 0 / 1         | 0 / 1           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | prepaid                                                     | 2.3.4.5   | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 |        | P:test    | 123               | 113             |       | 0           | 0 / 1         | 0 / 1           | connected | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                prepaid: false,
                solana_validator: false,
                solana_identity: None,
                edge_seat: false,
                tenant: None,
                json: false,
                json_compact: true,
                client_ip: None,
                user_payer: None,
                multicast_group_publisher: None,
                multicast_group_subscriber: None,
                not_multicast_group_publisher: None,
                not_multicast_group_subscriber: None,
                narrow: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"accesspass_type\":\"solana_validator: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"client_ip\":\"0.0.0.0\",\"user_payer\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"tenant\":\"\",\"multicast\":\"S:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"\",\"connections\":0,\"unicast_users\":\"0 / 1\",\"multicast_users\":\"0 / 1\",\"status\":\"Connected\",\"owner\":\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\"},{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"accesspass_type\":\"prepaid\",\"client_ip\":\"1.2.3.4\",\"user_payer\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"tenant\":\"\",\"multicast\":\"P:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"\",\"connections\":0,\"unicast_users\":\"0 / 1\",\"multicast_users\":\"0 / 1\",\"status\":\"Connected\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"},{\"account\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"accesspass_type\":\"prepaid\",\"client_ip\":\"2.3.4.5\",\"user_payer\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"tenant\":\"\",\"multicast\":\"P:test\",\"last_access_epoch\":\"123\",\"remaining_epoch\":\"113\",\"flags\":\"\",\"connections\":0,\"unicast_users\":\"0 / 1\",\"multicast_users\":\"0 / 1\",\"status\":\"Connected\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\"}]\n");

        // Test filtering by client IP
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                client_ip: Some(Ipv4Addr::new(1, 2, 3, 4)),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type | client_ip | user_payer                                | tenant | multicast | last_access_epoch | remaining_epoch | flags | connections | unicast_users | multicast_users | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid         | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB |        | P:test    | 123               | 113             |       | 0           | 0 / 1         | 0 / 1           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        // Test filtering by user payer
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                user_payer: Some(Pubkey::from_str_const(
                    "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB",
                )),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type | client_ip | user_payer                                | tenant | multicast | last_access_epoch | remaining_epoch | flags | connections | unicast_users | multicast_users | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | prepaid         | 1.2.3.4   | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB |        | P:test    | 123               | 113             |       | 0           | 0 / 1         | 0 / 1           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");
        // Narrow output: shortened pubkeys, abbreviated type/multicast, short
        // headers; fits within 240 cols.
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                narrow: true,
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let header = output_str.lines().next().unwrap();
        for expected in [
            "account",
            "accesspass_type",
            "client_ip",
            "user_payer",
            "tenant",
            "multicast",
            "lst_epch",
            "rem_epch",
            "conns",
            "status",
            "owner",
        ] {
            assert!(header.contains(expected), "missing header {expected}");
        }
        for hidden in ["last_access_epoch", "remaining_epoch", "connections"] {
            assert!(
                !header.contains(hidden),
                "narrow header should not contain {hidden}"
            );
        }
        // SolanaValidator type keeps its label but shortens the embedded key.
        assert!(output_str.contains("solana_validator: 1111111FVA.."));
        assert!(!output_str.contains("solana_validator: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"));
        for line in output_str.lines() {
            assert!(
                line.len() <= 240,
                "narrow line exceeds 240 cols: {}",
                line.len()
            );
        }
    }

    #[test]
    fn test_accesspass_type_short() {
        let pk = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::SolanaValidator(pk)),
            "solana_validator: 1111111FVA.."
        );
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::SolanaRPC(pk)),
            "solana_rpc: 1111111FVA.."
        );
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::Prepaid),
            "prepaid"
        );
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::EdgeSeat),
            "edge_seat"
        );
        // Others: type_name kept, embedded key truncated to a copyable prefix.
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::Others(
                "custom".to_string(),
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB".to_string()
            )),
            "others: custom (1111111FVA..)"
        );
        assert_eq!(
            super::accesspass_type_short(&AccessPassType::Others(
                "x".to_string(),
                "short".to_string()
            )),
            "others: x (short)"
        );
    }

    fn setup_multicast_client() -> (
        crate::doublezerocommand::MockCliCommand,
        Pubkey,
        Pubkey,
        Pubkey,
    ) {
        let mut client = create_test_client();

        let mgroup_pubkey = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        let mgroup = doublezero_sdk::MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            code: "test".to_string(),
            publisher_count: 5,
            subscriber_count: 10,
        };

        // access1: publisher of "test", IP 1.2.3.4
        let access1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let access1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: "1.2.3.4".parse().unwrap(),
            accesspass_type: AccessPassType::Prepaid,
            user_payer: access1_pubkey,
            last_access_epoch: 100,
            owner: access1_pubkey,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        // access2: subscriber of "test", IP 0.0.0.0
        let access2_pubkey = mgroup_pubkey;
        let access2 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: "0.0.0.0".parse().unwrap(),
            accesspass_type: AccessPassType::Prepaid,
            user_payer: access2_pubkey,
            last_access_epoch: 100,
            owner: access2_pubkey,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        // access3: publisher of "test", IP 2.3.4.5
        let access3_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let access3 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: "2.3.4.5".parse().unwrap(),
            accesspass_type: AccessPassType::Prepaid,
            user_payer: access3_pubkey,
            last_access_epoch: 100,
            owner: access3_pubkey,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = std::collections::HashMap::new();
            mgroups.insert(mgroup_pubkey, mgroup.clone());
            Ok(mgroups)
        });
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client.expect_list_accesspass().returning(move |_| {
            let mut passes = std::collections::HashMap::new();
            passes.insert(access1_pubkey, access1.clone());
            passes.insert(access2_pubkey, access2.clone());
            passes.insert(access3_pubkey, access3.clone());
            Ok(passes)
        });

        (client, access1_pubkey, access2_pubkey, access3_pubkey)
    }

    #[test]
    fn test_filter_multicast_group_publisher() {
        let (client, access1_pubkey, access2_pubkey, access3_pubkey) = setup_multicast_client();

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                multicast_group_publisher: Some("test".to_string()),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        // access1 and access3 have "test" in their pub_allowlist
        assert!(out.contains(&access1_pubkey.to_string()));
        assert!(out.contains(&access3_pubkey.to_string()));
        // access2 has no publishers — excluded
        assert!(!out.contains(&access2_pubkey.to_string()));
    }

    #[test]
    fn test_filter_multicast_group_subscriber() {
        let (client, access1_pubkey, access2_pubkey, access3_pubkey) = setup_multicast_client();

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                multicast_group_subscriber: Some("test".to_string()),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        // access2 has "test" in its sub_allowlist
        assert!(out.contains(&access2_pubkey.to_string()));
        // access1 and access3 have no subscribers — excluded
        assert!(!out.contains(&access1_pubkey.to_string()));
        assert!(!out.contains(&access3_pubkey.to_string()));
    }

    #[test]
    fn test_filter_not_multicast_group_publisher() {
        let (client, access1_pubkey, access2_pubkey, access3_pubkey) = setup_multicast_client();

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                not_multicast_group_publisher: Some("test".to_string()),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        // access2 does not have "test" in its pub_allowlist — retained
        assert!(out.contains(&access2_pubkey.to_string()));
        // access1 and access3 have "test" as a publisher — excluded
        assert!(!out.contains(&access1_pubkey.to_string()));
        assert!(!out.contains(&access3_pubkey.to_string()));
    }

    #[test]
    fn test_filter_not_multicast_group_subscriber() {
        let (client, access1_pubkey, access2_pubkey, access3_pubkey) = setup_multicast_client();

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListAccessPassCliCommand {
                not_multicast_group_subscriber: Some("test".to_string()),
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        // access1 and access3 do not have "test" in their sub_allowlist — retained
        assert!(out.contains(&access1_pubkey.to_string()));
        assert!(out.contains(&access3_pubkey.to_string()));
        // access2 has "test" as a subscriber — excluded
        assert!(!out.contains(&access2_pubkey.to_string()));
    }
}
