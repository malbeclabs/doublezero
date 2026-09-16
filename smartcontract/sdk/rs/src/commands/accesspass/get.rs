use std::net::Ipv4Addr;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    state::{accesspass::AccessPass, accountdata::AccountData, user::User},
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
}

/// Fetch the AccessPass stored at the exact `(client_ip, user_payer)` PDA.
///
/// Unlike [`GetAccessPassCommand`], this never falls back to the dynamic (0.0.0.0) pass.
/// `connect --client-ip` needs exactly that distinction: the flag is honored only for a pass
/// whose address an issuing authority pinned, and the wildcard-first resolution above would
/// answer such a query with a dynamic pass — one that authorizes any address at all, which is
/// the case the flag must refuse.
#[derive(Debug, PartialEq, Clone)]
pub struct GetExactAccessPassCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl GetExactAccessPassCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<Option<(Pubkey, AccessPass)>> {
        // A pass at the UNSPECIFIED PDA is the dynamic pass by construction, so an exact
        // lookup for it would contradict the name. Refuse rather than quietly resolving it.
        if self.client_ip == Ipv4Addr::UNSPECIFIED {
            return Ok(None);
        }
        let program_id = client.get_program_id();
        let (pubkey, _) = get_accesspass_pda(&program_id, &self.client_ip, &self.user_payer);
        match client.get(pubkey) {
            Ok(AccountData::AccessPass(accesspass)) => Ok(Some((pubkey, accesspass))),
            Ok(_) => Ok(None),
            // `GetAccessPassCommand` can fold an error into `None` because a second lookup
            // follows it; here the lookup *is* the answer, so an unreachable or misconfigured
            // ledger would otherwise render as "this payer holds no pass" and send an operator
            // off to have one reissued. Only a genuinely absent account is `None`. The RPC
            // reports that as `AccountNotFound` and offers no typed form of it through this
            // trait, so the string is what there is to match; misreading one as absent is the
            // behaviour this replaces, and the transport errors worth retrying have already
            // been retried by the client.
            Err(err) => {
                if format!("{err:#}").contains("AccountNotFound") {
                    Ok(None)
                } else {
                    Err(err).wrap_err_with(|| {
                        format!("reading the AccessPass at {pubkey} for {}", self.user_payer)
                    })
                }
            }
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
        commands::accesspass::get::{
            resolve_user_accesspass, GetAccessPassCommand, GetExactAccessPassCommand,
        },
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            user::User,
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

    /// The whole point of the exact command: a dynamic pass must not answer for an address.
    #[test]
    fn test_get_exact_accesspass_never_resolves_the_dynamic_pass() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let client_ip: Ipv4Addr = [203, 0, 113, 9].into();
        let payer = Pubkey::new_unique();

        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        // Only the exact PDA is ever queried, and it holds nothing.
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .times(1)
            .returning(|_| Ok(AccountData::None));

        let res = GetExactAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .expect("a missing account is not an error");
        assert!(res.is_none());
    }

    #[test]
    fn test_get_exact_accesspass_returns_the_pass_at_that_pda() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let client_ip: Ipv4Addr = [203, 0, 113, 9].into();
        let payer = Pubkey::new_unique();

        let (exact_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let pass = sample_accesspass(client_ip, payer);
        let expected = pass.clone();
        client
            .expect_get()
            .with(predicate::eq(exact_pubkey))
            .times(1)
            .returning(move |_| Ok(AccountData::AccessPass(pass.clone())));

        let (pubkey, found) = GetExactAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .expect("the lookup must succeed")
        .expect("the pass must be found");
        assert_eq!(pubkey, exact_pubkey);
        assert_eq!(found, expected);
    }

    /// An unreachable or misconfigured ledger must not read as "this payer holds no pass" —
    /// that is the answer that sends an operator to have a live pass reissued.
    #[test]
    fn test_get_exact_accesspass_propagates_a_transport_error() {
        let mut client = create_test_client();
        let client_ip: Ipv4Addr = [203, 0, 113, 9].into();
        let payer = Pubkey::new_unique();

        client
            .expect_get()
            .times(1)
            .returning(|_| Err(eyre::eyre!("error sending request for url (http://ledger)")));

        let err = GetExactAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .expect_err("a transport failure must not be reported as an absent pass");
        assert!(
            format!("{err:#}").contains("reading the AccessPass at"),
            "unexpected error: {err:#}"
        );
    }

    /// The RPC's own way of saying the account is not there still means "no pass".
    #[test]
    fn test_get_exact_accesspass_treats_account_not_found_as_absent() {
        let mut client = create_test_client();
        let client_ip: Ipv4Addr = [203, 0, 113, 9].into();
        let payer = Pubkey::new_unique();

        client.expect_get().times(1).returning(|pk| {
            Err(eyre::eyre!(
                "AccountNotFound: pubkey={pk}: RPC response error"
            ))
        });

        let res = GetExactAccessPassCommand {
            client_ip,
            user_payer: payer,
        }
        .execute(&client)
        .expect("AccountNotFound is absence, not failure");
        assert!(res.is_none());
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
