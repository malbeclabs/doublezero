use std::net::Ipv4Addr;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    state::{
        accesspass::AccessPass,
        accountdata::AccountData,
        user::{epoch_allows_connection, User, UserType},
    },
};
use eyre::WrapErr;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetAccessPassCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl GetAccessPassCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<Option<(Pubkey, AccessPass)>> {
        let program_id = client.get_program_id();

        // Prefer a shared dynamic seat pass stored at the UNSPECIFIED (0.0.0.0) PDA,
        // aligning the read path with the onchain create_user path (which accepts either
        // the exact-IP PDA or the UNSPECIFIED PDA). Fall back to the exact-IP pass only
        // when the dynamic pass is absent.
        if self.client_ip != Ipv4Addr::UNSPECIFIED {
            let (dynamic_pubkey, _) =
                get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &self.user_payer);
            if let Ok(AccountData::AccessPass(accesspass)) = client.get(dynamic_pubkey) {
                return Ok(Some((dynamic_pubkey, accesspass)));
            }
        }

        let (pubkey, _) = get_accesspass_pda(&program_id, &self.client_ip, &self.user_payer);
        match client.get(pubkey) {
            Ok(AccountData::AccessPass(accesspass)) => Ok(Some((pubkey, accesspass))),
            Ok(_) | Err(_) => Ok(None),
        }
    }

    /// Like `execute`, but for a caller about to create a `user_type` user: prefers whichever of
    /// the dynamic (UNSPECIFIED) and exact-IP candidates actually clears the access-pass epoch
    /// check for that user type, instead of always taking the dynamic pass when one exists.
    ///
    /// A dynamic pass is not epoch-gated to begin with when it only ever backed a multicast
    /// (EdgeSeat) subscription, so its `last_access_epoch` can go stale while an exact-IP prepaid
    /// pass at the same address is still valid. `execute` would still return the stale dynamic
    /// pass, and the caller's own epoch check would then reject it even though a usable pass
    /// exists — this evaluates both candidates against the epoch check the on-chain program
    /// actually enforces, so the pass this returns is the same one a subsequent create_user /
    /// create_subscribe_user call would need to succeed.
    pub fn execute_usable(
        &self,
        client: &dyn DoubleZeroClient,
        user_type: UserType,
    ) -> eyre::Result<Option<(Pubkey, AccessPass)>> {
        let program_id = client.get_program_id();

        let dynamic = if self.client_ip != Ipv4Addr::UNSPECIFIED {
            let (dynamic_pubkey, _) =
                get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &self.user_payer);
            match client.get(dynamic_pubkey) {
                Ok(AccountData::AccessPass(accesspass)) => Some((dynamic_pubkey, accesspass)),
                _ => None,
            }
        } else {
            None
        };

        // No dynamic pass (or the requested IP already is UNSPECIFIED, so the exact-IP PDA below
        // *is* the dynamic one): nothing to arbitrate between, same as `execute`.
        let Some(dynamic) = dynamic else {
            let (exact_pubkey, _) =
                get_accesspass_pda(&program_id, &self.client_ip, &self.user_payer);
            return Ok(match client.get(exact_pubkey) {
                Ok(AccountData::AccessPass(accesspass)) => Some((exact_pubkey, accesspass)),
                _ => None,
            });
        };

        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &self.client_ip, &self.user_payer);
        let exact = match client.get(exact_pubkey) {
            Ok(AccountData::AccessPass(accesspass)) => Some((exact_pubkey, accesspass)),
            _ => None,
        };

        // No exact-IP pass either: only one candidate exists, same as `execute`.
        let Some(exact) = exact else {
            return Ok(Some(dynamic));
        };

        // Both candidates exist: an epoch read is unavoidable to tell which one a subsequent
        // create_user / create_subscribe_user call would actually be able to use.
        let current_epoch = client.get_epoch()?;
        let is_usable = |accesspass: &AccessPass| {
            epoch_allows_connection(user_type, accesspass.last_access_epoch, current_epoch)
        };

        if is_usable(&dynamic.1) {
            Ok(Some(dynamic))
        } else if is_usable(&exact.1) {
            Ok(Some(exact))
        } else {
            // Neither clears the epoch check; keep `execute`'s dynamic-first default so the
            // resulting on-chain failure names the same pass the caller's own preflight saw.
            Ok(Some(dynamic))
        }
    }
}

pub fn resolve_user_accesspass(
    client: &dyn DoubleZeroClient,
    user_pk: Pubkey,
    user: &User,
    selected_accesspass_pk: Option<Pubkey>,
) -> eyre::Result<(Pubkey, AccessPass)> {
    if user.accesspass_pk != Pubkey::default() {
        if let Some(selected_pk) = selected_accesspass_pk {
            if selected_pk != user.accesspass_pk {
                eyre::bail!(
                    "User {user_pk} records access pass {}. Remove --access-pass or provide that address.",
                    user.accesspass_pk
                );
            }
        }

        return match client.get(user.accesspass_pk) {
            Ok(AccountData::AccessPass(accesspass)) => Ok((user.accesspass_pk, accesspass)),
            Ok(_) => eyre::bail!(
                "Recorded access pass {} for user {user_pk} has the wrong account type",
                user.accesspass_pk,
            ),
            Err(err) => Err(err).wrap_err_with(|| {
                format!(
                    "Failed to load recorded access pass {} for user {user_pk}",
                    user.accesspass_pk
                )
            }),
        };
    }

    let candidates = legacy_user_accesspass_candidates(client, user);

    if let Some(selected_pk) = selected_accesspass_pk {
        return candidates
            .into_iter()
            .find(|(pk, _)| *pk == selected_pk)
            .ok_or_else(|| {
                eyre::eyre!("Access pass {selected_pk} does not match legacy user {user_pk}")
            });
    }

    match candidates.as_slice() {
        [] => eyre::bail!("No access pass matches legacy user {user_pk}"),
        [candidate] => Ok(candidate.clone()),
        _ => {
            let choices = format_accesspass_choices(&candidates);
            eyre::bail!(
                "Legacy user {user_pk} matches multiple access passes:\n{choices}\nRetry with --access-pass <ADDRESS>."
            )
        }
    }
}

fn legacy_user_accesspass_candidates(
    client: &dyn DoubleZeroClient,
    user: &User,
) -> Vec<(Pubkey, AccessPass)> {
    let program_id = client.get_program_id();
    let (exact_pk, _) = get_accesspass_pda(&program_id, &user.client_ip, &user.owner);
    let (dynamic_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    let candidate_pks = if exact_pk == dynamic_pk {
        vec![exact_pk]
    } else {
        vec![exact_pk, dynamic_pk]
    };
    candidate_pks
        .into_iter()
        .filter_map(|pk| match client.get(pk) {
            Ok(AccountData::AccessPass(accesspass)) if accesspass.user_payer == user.owner => {
                Some((pk, accesspass))
            }
            _ => None,
        })
        .collect()
}

fn format_accesspass_choices(candidates: &[(Pubkey, AccessPass)]) -> String {
    candidates
        .iter()
        .map(|(pk, accesspass)| {
            format!(
                "  {pk}: {:?}, client IP {}, {} connections",
                accesspass.accesspass_type, accesspass.client_ip, accesspass.connection_count
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::get::{resolve_user_accesspass, GetAccessPassCommand},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            user::{User, UserType},
        },
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    fn sample_accesspass(client_ip: Ipv4Addr, user_payer: Pubkey) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        }
    }

    #[test]
    fn test_get_accesspass_prefers_dynamic_pass() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);

        // The dynamic (UNSPECIFIED) PDA resolves first; the exact-IP PDA is never queried.
        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .times(1)
            .returning(move |_| Ok(AccountData::AccessPass(dynamic_pass.clone())));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .unwrap();

        let (pubkey, pass) = res.expect("expected a pass");
        assert_eq!(pubkey, dynamic_pubkey);
        assert_eq!(pass.client_ip, Ipv4Addr::UNSPECIFIED);
    }

    #[test]
    fn test_get_accesspass_falls_back_to_exact_ip() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let exact_pass = sample_accesspass(client_ip, payer);

        // No dynamic pass exists; fall back to the exact-IP pass.
        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(exact_pass.clone())));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .unwrap();

        let (pubkey, pass) = res.expect("expected a pass");
        assert_eq!(pubkey, exact_pubkey);
        assert_eq!(pass.client_ip, client_ip);
    }

    #[test]
    fn test_get_accesspass_returns_none_when_neither_present() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);

        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .unwrap();

        assert!(res.is_none());
    }

    #[test]
    fn test_get_accesspass_unspecified_does_single_lookup() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let payer = Pubkey::new_unique();
        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);

        // When the requested IP is already UNSPECIFIED, only a single lookup happens
        // (the exact-IP PDA is the UNSPECIFIED PDA — no redundant double query).
        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .times(1)
            .returning(move |_| Ok(AccountData::AccessPass(dynamic_pass.clone())));

        let res = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
        }
        .execute(&client)
        .unwrap();

        let (pubkey, _) = res.expect("expected a pass");
        assert_eq!(pubkey, dynamic_pubkey);
    }

    #[test]
    fn test_execute_usable_returns_exact_when_dynamic_is_stale() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        // The dynamic pass exists but its epoch has passed (e.g. left over from a
        // never-epoch-gated EdgeSeat multicast subscription); the exact-IP pass is
        // still within its epoch. execute_usable must prefer the exact-IP one, unlike
        // execute (see test_get_accesspass_prefers_dynamic_pass).
        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let stale_dynamic_pass = AccessPass {
            last_access_epoch: 5,
            ..sample_accesspass(Ipv4Addr::UNSPECIFIED, payer)
        };
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let usable_exact_pass = sample_accesspass(client_ip, payer);

        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(stale_dynamic_pass.clone())));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(usable_exact_pass.clone())));
        client.expect_get_epoch().returning(|| Ok(10));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute_usable(&client, UserType::IBRL)
        .unwrap();

        let (pubkey, pass) = res.expect("expected a pass");
        assert_eq!(pubkey, exact_pubkey);
        assert_eq!(pass.client_ip, client_ip);
    }

    #[test]
    fn test_execute_usable_prefers_dynamic_when_both_are_usable() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let exact_pass = sample_accesspass(client_ip, payer);

        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(dynamic_pass.clone())));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(exact_pass.clone())));
        client.expect_get_epoch().returning(|| Ok(10));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute_usable(&client, UserType::IBRL)
        .unwrap();

        let (pubkey, _) = res.expect("expected a pass");
        assert_eq!(pubkey, dynamic_pubkey);
    }

    #[test]
    fn test_execute_usable_falls_back_to_dynamic_when_neither_clears_epoch_check() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let stale_dynamic_pass = AccessPass {
            last_access_epoch: 1,
            ..sample_accesspass(Ipv4Addr::UNSPECIFIED, payer)
        };
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let stale_exact_pass = AccessPass {
            last_access_epoch: 2,
            ..sample_accesspass(client_ip, payer)
        };

        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(stale_dynamic_pass.clone())));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(stale_exact_pass.clone())));
        client.expect_get_epoch().returning(|| Ok(10));

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute_usable(&client, UserType::IBRL)
        .unwrap();

        let (pubkey, _) = res.expect("expected a pass");
        assert_eq!(pubkey, dynamic_pubkey);
    }

    #[test]
    fn test_execute_usable_skips_epoch_read_when_only_one_candidate_exists() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();

        let client_ip: Ipv4Addr = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (dynamic_pubkey, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);
        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);

        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(dynamic_pass.clone())));
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));
        // No expect_get_epoch() set: with only one candidate present, execute_usable
        // must not need an epoch read to decide — a call would panic (unmocked).

        let res = GetAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute_usable(&client, UserType::IBRL)
        .unwrap();

        let (pubkey, _) = res.expect("expected a pass");
        assert_eq!(pubkey, dynamic_pubkey);
    }

    #[test]
    fn test_resolve_user_accesspass_uses_recorded_address() {
        let mut client = create_test_client();
        let user_pk = Pubkey::new_unique();
        let accesspass_pk = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = User {
            owner: payer,
            accesspass_pk,
            ..Default::default()
        };
        let accesspass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);
        let expected_accesspass = accesspass.clone();

        client
            .expect_get()
            .with(predicate::eq(accesspass_pk))
            .times(1)
            .return_once(move |_| Ok(AccountData::AccessPass(accesspass)));

        let resolved = resolve_user_accesspass(&client, user_pk, &user, None).unwrap();
        assert_eq!(resolved, (accesspass_pk, expected_accesspass));
    }

    #[test]
    fn test_resolve_user_accesspass_uses_single_legacy_candidate() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let user_pk = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let user = User {
            owner: payer,
            client_ip,
            ..Default::default()
        };
        let (exact_pk, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let (dynamic_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);
        let expected_accesspass = dynamic_pass.clone();

        client
            .expect_get()
            .with(predicate::eq(exact_pk))
            .return_once(|_| Err(eyre::eyre!("account not found")));
        client
            .expect_get()
            .with(predicate::eq(dynamic_pk))
            .return_once(move |_| Ok(AccountData::AccessPass(dynamic_pass)));

        let resolved = resolve_user_accesspass(&client, user_pk, &user, None).unwrap();

        assert_eq!(resolved, (dynamic_pk, expected_accesspass));
    }

    // Legacy users record no pass. When both possible passes exist, deletion must ask the caller
    // to select one.
    #[test]
    fn test_resolve_user_accesspass_lists_legacy_conflict() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let user_pk = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let user = User {
            owner: payer,
            client_ip,
            ..Default::default()
        };
        let (exact_pk, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let (dynamic_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let exact_pass = sample_accesspass(client_ip, payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);

        client
            .expect_get()
            .with(predicate::eq(exact_pk))
            .return_once(move |_| Ok(AccountData::AccessPass(exact_pass)));
        client
            .expect_get()
            .with(predicate::eq(dynamic_pk))
            .return_once(move |_| Ok(AccountData::AccessPass(dynamic_pass)));

        let error = resolve_user_accesspass(&client, user_pk, &user, None).unwrap_err();
        let message = error.to_string();
        let lines = message.lines().collect::<Vec<_>>();
        assert_eq!(lines.len(), 4);
        assert_eq!(
            lines[0],
            format!("Legacy user {user_pk} matches multiple access passes:")
        );
        assert_eq!(
            lines[1],
            format!("  {exact_pk}: Prepaid, client IP {client_ip}, 0 connections")
        );
        assert_eq!(
            lines[2],
            format!("  {dynamic_pk}: Prepaid, client IP 0.0.0.0, 0 connections")
        );
        assert_eq!(lines[3], "Retry with --access-pass <ADDRESS>.");
    }

    // The caller resolves a legacy conflict by selecting either matching pass explicitly.
    #[test]
    fn test_resolve_user_accesspass_selects_legacy_candidate() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let user_pk = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let user = User {
            owner: payer,
            client_ip,
            ..Default::default()
        };
        let (exact_pk, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let (dynamic_pk, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let exact_pass = sample_accesspass(client_ip, payer);
        let dynamic_pass = sample_accesspass(Ipv4Addr::UNSPECIFIED, payer);
        let expected_accesspass = dynamic_pass.clone();

        client
            .expect_get()
            .with(predicate::eq(exact_pk))
            .return_once(move |_| Ok(AccountData::AccessPass(exact_pass)));
        client
            .expect_get()
            .with(predicate::eq(dynamic_pk))
            .return_once(move |_| Ok(AccountData::AccessPass(dynamic_pass)));

        let resolved = resolve_user_accesspass(&client, user_pk, &user, Some(dynamic_pk)).unwrap();
        assert_eq!(resolved, (dynamic_pk, expected_accesspass));
    }
}
