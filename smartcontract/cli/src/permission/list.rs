use crate::{doublezerocommand::CliCommand, permission::flags::bitmask_to_names};
use clap::Args;
use doublezero_cli_core::{render_collection, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::permission::list::ListPermissionCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct ListPermissionCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

/// Wraps `Vec<String>` to display as comma-separated in tables and as a JSON array.
#[derive(Serialize)]
#[serde(transparent)]
pub struct PermissionList(pub Vec<String>);

impl std::fmt::Display for PermissionList {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0.join(", "))
    }
}

#[derive(Tabled, Serialize)]
pub struct PermissionDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
    pub permissions: PermissionList,
    pub status: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListPermissionCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let permissions = client.list_permission(ListPermissionCommand {})?;

        let mut displays: Vec<PermissionDisplay> = permissions
            .into_iter()
            .map(|(pubkey, p)| PermissionDisplay {
                account: pubkey,
                user_payer: p.user_payer,
                permissions: PermissionList(bitmask_to_names(p.permissions)),
                status: p.status.to_string(),
                owner: p.owner,
            })
            .collect();

        displays.sort_by_key(|d| d.user_payer.to_string());

        render_collection(
            out,
            displays,
            OutputFormat::from_flags(self.json, self.json_compact),
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{permission::list::ListPermissionCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::cli_context_default_for_tests;
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::permission::{
        permission_flags, Permission, PermissionStatus,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;
    use tokio::runtime::Builder;

    fn block_on<F: std::future::Future>(f: F) -> F::Output {
        Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(f)
    }

    #[test]
    fn test_cli_permission_list() {
        let mut client = create_test_client();

        let pda = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: pda,
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer,
            permissions: permission_flags::NETWORK_ADMIN | permission_flags::ACTIVATOR,
        };

        let p2 = permission.clone();
        client
            .expect_list_permission()
            .returning(move |_| Ok(HashMap::from([(pda, p2.clone())])));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListPermissionCliCommand {
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        assert!(out.contains("activated"));
        assert!(out.contains("network-admin"));
        assert!(out.contains("activator"));
    }
}
