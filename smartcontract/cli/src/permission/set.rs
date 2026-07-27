use crate::{
    doublezerocommand::CliCommand,
    permission::flags::{bitmask_to_names, names_to_bitmask, PermissionName},
};
use clap::Args;
use doublezero_cli_core::{require, CliContext, RequirementCheck};
use doublezero_sdk::commands::permission::{
    create::CreatePermissionCommand, get::GetPermissionCommand, update::UpdatePermissionCommand,
};
use doublezero_serviceability::{authorize::AUTHORIZE_GATED_FLAGS, pda::get_permission_pda};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct SetPermissionCliCommand {
    /// Pubkey to grant/update permissions for
    #[arg(long)]
    pub user_payer: String,
    /// Permission(s) to grant (repeat for multiple: --add network-admin --add user-admin)
    #[arg(long = "add", value_name = "PERMISSION")]
    pub add: Vec<PermissionName>,
    /// Permission(s) to revoke — only valid when updating an existing account
    #[arg(long = "remove", value_name = "PERMISSION")]
    pub remove: Vec<PermissionName>,
}

impl SetPermissionCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        if self.add.is_empty() && self.remove.is_empty() {
            return Err(eyre::eyre!(
                "at least one --add or --remove flag is required"
            ));
        }

        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        let user_payer = Pubkey::from_str(&self.user_payer)
            .map_err(|e| eyre::eyre!("invalid user_payer pubkey: {e}"))?;

        let program_id = client.get_program_id();
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let existing = client
            .get_permission(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            })
            .ok();

        let (signature, new_permissions) = match existing {
            None => {
                if !self.remove.is_empty() {
                    return Err(eyre::eyre!(
                        "cannot --remove permissions from an account that does not exist yet"
                    ));
                }
                let permissions = names_to_bitmask(&self.add);
                let (sig, _) = client.create_permission(CreatePermissionCommand {
                    user_payer,
                    permissions,
                })?;
                (sig, permissions)
            }
            Some(_) => {
                // The program blocks any self-modification of an existing Permission
                // account (recovery is foundation-only). Surface that as an actionable
                // CLI error instead of an opaque on-chain revert. The create path (None
                // arm) is intentionally exempt: there is no account to lock yourself out
                // of yet, so bootstrapping your own key is allowed.
                if user_payer == client.get_payer() {
                    return Err(eyre::eyre!(
                        "cannot modify your own permission account: self-lockout is \
                         blocked and recovery is foundation-only — have another \
                         PERMISSION_ADMIN key make this change"
                    ));
                }
                let add = names_to_bitmask(&self.add);
                let remove = names_to_bitmask(&self.remove);
                let sig = client.update_permission(UpdatePermissionCommand {
                    permission_pda,
                    add,
                    remove,
                })?;
                let (_, updated) = client.get_permission(GetPermissionCommand {
                    pubkey: permission_pda.to_string(),
                })?;
                (sig, updated.permissions)
            }
        };

        // Two-line bespoke output (aligned "Signature:" + "Permissions:") rather
        // than the canonical `print_signature` so the permission summary stays
        // visually paired with the signature line.
        writeln!(out, "Signature:   {signature}")?;
        writeln!(
            out,
            "Permissions: {}",
            bitmask_to_names(new_permissions).join(", ")
        )?;

        if let Some(warning) = inert_grant_warning(new_permissions) {
            tracing::warn!("{warning}");
        }

        Ok(())
    }
}

/// Warn that an `authorize()`-gated grant is currently inert through this CLI.
///
/// The Rust SDK does not attach the payer's Permission PDA to serviceability
/// transactions (the RFC-26 builders derive account layout offline and their
/// Permission append is still deferred), so `authorize()` never sees the account
/// and falls back to the GlobalState allowlists. A key whose only grant is a
/// Permission account therefore still gets `NotAllowed`. Note `permission audit`
/// reports the inverse (strict-mode) direction only, so nothing else surfaces this.
///
/// Returns `None` when the resulting bitmask carries no `authorize()`-gated flag,
/// so grants that only touch un-migrated subsystems stay quiet.
///
/// Remove this once the builder-side Permission append is re-enabled.
fn inert_grant_warning(permissions: u128) -> Option<&'static str> {
    let gated: u128 = AUTHORIZE_GATED_FLAGS.iter().fold(0, |acc, f| acc | f);
    (permissions & gated != 0).then_some(
        "this grant does not take effect through the doublezero CLI or Rust SDK yet: they do \
         not attach Permission accounts to serviceability transactions, so authorize() falls \
         back to the GlobalState allowlists. The key still needs a foundation_allowlist / \
         qa_allowlist / authority entry to exercise gated commands.",
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        permission::flags::PermissionName,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        permission::{permission_flags, Permission, PermissionStatus},
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    const TEST_PROGRAM_ID: Pubkey =
        Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");

    fn make_permission(permissions: u128) -> Permission {
        Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status: PermissionStatus::Activated,
            user_payer: Pubkey::new_unique(),
            permissions,
        }
    }

    #[test]
    fn test_set_creates_when_account_absent() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);
        let permissions = permission_flags::NETWORK_ADMIN | permission_flags::USER_ADMIN;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .returning(|_| Err(eyre::eyre!("not found")));
        client
            .expect_create_permission()
            .with(predicate::eq(CreatePermissionCommand {
                user_payer,
                permissions,
            }))
            .returning(move |_| Ok((Signature::new_unique(), permission_pda)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![PermissionName::NetworkAdmin, PermissionName::UserAdmin],
                remove: vec![],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        assert!(out.contains("Signature:"));
        assert!(out.contains("network-admin"));
        assert!(out.contains("user-admin"));
    }

    #[test]
    fn test_set_remove_on_nonexistent_account_rejected() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .returning(|_| Err(eyre::eyre!("not found")));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![PermissionName::NetworkAdmin],
                remove: vec![PermissionName::UserAdmin],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("cannot --remove permissions from an account that does not exist yet"));
    }

    #[test]
    fn test_set_updates_when_account_exists_add() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);

        let initial = permission_flags::NETWORK_ADMIN;
        let updated = initial | permission_flags::SENTINEL;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(initial))));
        client
            .expect_update_permission()
            .with(predicate::eq(UpdatePermissionCommand {
                permission_pda,
                add: permission_flags::SENTINEL,
                remove: 0,
            }))
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(updated))));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![PermissionName::Sentinel],
                remove: vec![],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        assert!(out.contains("network-admin"));
        assert!(out.contains("sentinel"));
    }

    #[test]
    fn test_set_updates_when_account_exists_remove() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);

        let initial = permission_flags::NETWORK_ADMIN | permission_flags::USER_ADMIN;
        let updated = permission_flags::NETWORK_ADMIN;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(initial))));
        client
            .expect_update_permission()
            .with(predicate::eq(UpdatePermissionCommand {
                permission_pda,
                add: 0,
                remove: permission_flags::USER_ADMIN,
            }))
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(updated))));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![],
                remove: vec![PermissionName::UserAdmin],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
        let out = String::from_utf8(output).unwrap();
        assert!(out.contains("network-admin"));
        assert!(!out.contains("user-admin"));
    }

    #[test]
    fn test_set_updates_add_and_remove() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);

        let initial = permission_flags::NETWORK_ADMIN | permission_flags::USER_ADMIN;
        let updated = permission_flags::NETWORK_ADMIN | permission_flags::SENTINEL;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(initial))));
        client
            .expect_update_permission()
            .with(predicate::eq(UpdatePermissionCommand {
                permission_pda,
                add: permission_flags::SENTINEL,
                remove: permission_flags::USER_ADMIN,
            }))
            .returning(|_| Ok(Signature::new_unique()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .once()
            .returning(move |_| Ok((permission_pda, make_permission(updated))));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![PermissionName::Sentinel],
                remove: vec![PermissionName::UserAdmin],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_ok());
    }

    #[test]
    fn test_set_self_modification_rejected() {
        // The signer targets its own key on the update path (account exists): the CLI
        // must reject early with an actionable message rather than emit an on-chain tx.
        let mut client = create_test_client();
        // Matches the payer wired into create_test_client().
        let self_payer = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &self_payer);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_permission()
            .with(predicate::eq(GetPermissionCommand {
                pubkey: permission_pda.to_string(),
            }))
            .returning(move |_| {
                Ok((
                    permission_pda,
                    make_permission(permission_flags::PERMISSION_ADMIN),
                ))
            });

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: self_payer.to_string(),
                add: vec![],
                remove: vec![PermissionName::PermissionAdmin],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("cannot modify your own permission account"));
    }

    #[test]
    fn test_inert_grant_warning_fires_only_for_authorize_gated_flags() {
        // Gated: the grant is real on-chain but unusable through this CLI until the
        // builder-side Permission append is re-enabled.
        for &flag in AUTHORIZE_GATED_FLAGS {
            assert!(
                inert_grant_warning(flag).is_some(),
                "expected a warning for gated flag {flag:#x}"
            );
        }
        // Not gated: no processor hands these to authorize(), so a Permission account
        // never authorized them in the first place — nothing to warn about.
        assert!(inert_grant_warning(permission_flags::SENTINEL).is_none());
        assert!(inert_grant_warning(permission_flags::QA).is_none());
        assert!(inert_grant_warning(permission_flags::FEED_AUTHORITY).is_none());
        assert!(inert_grant_warning(0).is_none());
        // A mixed bitmask still warns on the gated bit.
        assert!(
            inert_grant_warning(permission_flags::SENTINEL | permission_flags::NETWORK_ADMIN)
                .is_some()
        );
    }

    #[test]
    fn test_set_no_flags_rejected() {
        let client = create_test_client();
        let user_payer = Pubkey::new_unique();

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            SetPermissionCliCommand {
                user_payer: user_payer.to_string(),
                add: vec![],
                remove: vec![],
            }
            .execute(&ctx, &client, &mut output),
        );

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("at least one --add or --remove flag is required"));
    }
}
