use crate::{doublezerocommand::CliCommand, permission::flags::bitmask_to_names};
use clap::Args;
use doublezero_cli_core::{render_record, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::permission::get::GetPermissionCommand;
use doublezero_serviceability::pda::get_permission_pda;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetPermissionCliCommand {
    /// Pubkey to look up permissions for
    #[arg(long)]
    pub user_payer: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct PermissionDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
    pub permissions: PermissionList,
    pub status: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

/// Wraps `Vec<String>` to display as comma-separated in tables and as a JSON array.
#[derive(Serialize)]
#[serde(transparent)]
struct PermissionList(Vec<String>);

impl std::fmt::Display for PermissionList {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0.join(", "))
    }
}

impl GetPermissionCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let user_payer = Pubkey::from_str(&self.user_payer)
            .map_err(|e| eyre::eyre!("invalid user_payer pubkey: {e}"))?;

        let program_id = client.get_program_id();
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let (pubkey, permission) = client.get_permission(GetPermissionCommand {
            pubkey: permission_pda.to_string(),
        })?;

        let display = PermissionDisplay {
            account: pubkey,
            user_payer: permission.user_payer,
            permissions: PermissionList(bitmask_to_names(permission.permissions)),
            status: permission.status.to_string(),
            owner: permission.owner,
        };

        render_record(out, &display, OutputFormat::from_flags(self.json, false))
    }
}

#[cfg(test)]
mod tests {
    use crate::{permission::get::GetPermissionCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::cli_context_default_for_tests;
    use doublezero_sdk::{commands::permission::get::GetPermissionCommand, AccountType};
    use doublezero_serviceability::{
        pda::get_permission_pda,
        state::permission::{permission_flags, Permission, PermissionStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use tokio::runtime::Builder;

    const TEST_PROGRAM_ID: Pubkey =
        Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");

    fn block_on<F: std::future::Future>(f: F) -> F::Output {
        Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(f)
    }

    #[test]
    fn test_cli_permission_get() {
        let mut client = create_test_client();

        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);
        let permission = Permission {
            account_type: AccountType::Permission,
            owner: permission_pda,
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer,
            permissions: permission_flags::NETWORK_ADMIN,
        };

        let p2 = permission.clone();
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .returning(move |_| Ok((permission_pda, p2.clone())));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        assert!(out.contains("activated"));
        assert!(out.contains("network-admin"));
    }
}
