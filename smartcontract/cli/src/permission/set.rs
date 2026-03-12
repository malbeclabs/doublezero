use crate::{
    doublezerocommand::CliCommand,
    permission::flags::{bitmask_to_names, names_to_bitmask, PermissionName},
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::permission::{
    create::CreatePermissionCommand, get::GetPermissionCommand, update::UpdatePermissionCommand,
};
use doublezero_serviceability::pda::get_permission_pda;
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
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        if self.add.is_empty() && self.remove.is_empty() {
            return Err(eyre::eyre!(
                "at least one --add or --remove flag is required"
            ));
        }

        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

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
                // Account does not exist — create it.
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
                // Account exists — apply incremental delta atomically via the program.
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

        writeln!(out, "Signature:   {signature}")?;
        writeln!(
            out,
            "Permissions: {}",
            bitmask_to_names(new_permissions).join(", ")
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        permission::flags::PermissionName,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        permission::{permission_flags, Permission, PermissionStatus},
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    // Fixed program_id set by create_test_client().
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

    // ── create path ──────────────────────────────────────────────────────────

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

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![PermissionName::NetworkAdmin, PermissionName::UserAdmin],
            remove: vec![],
        }
        .execute(&client, &mut output);

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

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![PermissionName::NetworkAdmin],
            remove: vec![PermissionName::UserAdmin],
        }
        .execute(&client, &mut output);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("cannot --remove permissions from an account that does not exist yet"));
    }

    // ── update path ──────────────────────────────────────────────────────────

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

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![PermissionName::Sentinel],
            remove: vec![],
        }
        .execute(&client, &mut output);

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

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![],
            remove: vec![PermissionName::UserAdmin],
        }
        .execute(&client, &mut output);

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

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![PermissionName::Sentinel],
            remove: vec![PermissionName::UserAdmin],
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
    }

    #[test]
    fn test_set_no_flags_rejected() {
        let client = create_test_client();
        let user_payer = Pubkey::new_unique();

        let mut output = Vec::new();
        let res = SetPermissionCliCommand {
            user_payer: user_payer.to_string(),
            add: vec![],
            remove: vec![],
        }
        .execute(&client, &mut output);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("at least one --add or --remove flag is required"));
    }
}
