pub mod publisher;
pub mod subscriber;

use std::net::Ipv4Addr;

use doublezero_serviceability::{pda::get_accesspass_pda, state::accountdata::AccountData};
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

/// Resolves which AccessPass PDA to use for allowlist operations.
///
/// Prefers the dynamic PDA (`0.0.0.0`, `user_payer`): a pass stored at the `UNSPECIFIED` PDA is
/// valid for any client IP by construction (see the onchain `create_user` lookup in
/// `create_core.rs`), so it takes precedence over an exact-IP pass independent of
/// `allow_multiple_ip`. Falls back to the static PDA (`client_ip`, `user_payer`) when no dynamic
/// pass exists — either to use an existing exact-IP pass or as the creation target when neither
/// pass exists.
///
/// This mirrors `GetAccessPassCommand::execute` so allowlist ops and reads resolve the same pass.
pub(crate) fn resolve_accesspass_pda(
    client: &dyn DoubleZeroClient,
    client_ip: &Ipv4Addr,
    user_payer: &Pubkey,
) -> Pubkey {
    let program_id = client.get_program_id();

    let (dynamic_pda, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, user_payer);
    if let Ok(AccountData::AccessPass(_)) = client.get(dynamic_pda) {
        return dynamic_pda;
    }

    let (static_pda, _) = get_accesspass_pda(&program_id, client_ip, user_payer);
    static_pda
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
        },
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    use crate::{tests::utils::create_test_client, DoubleZeroClient};

    use super::resolve_accesspass_pda;

    fn make_accesspass(
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
        bump_seed: u8,
        flags: u8,
    ) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        }
    }

    #[test]
    fn test_resolve_accesspass_pda_prefers_dynamic() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let client_ip: Ipv4Addr = [192, 168, 1, 1].into();
        let user_payer = Pubkey::new_unique();
        let (dynamic_pda, bump) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user_payer);

        // flags=0 (no allow_multiple_ip): a pass at the UNSPECIFIED PDA still takes precedence,
        // and the exact-IP PDA is never queried.
        let ap = make_accesspass(Ipv4Addr::UNSPECIFIED, user_payer, bump, 0);
        client
            .expect_get()
            .with(predicate::eq(dynamic_pda))
            .times(1)
            .returning(move |_| Ok(AccountData::AccessPass(ap.clone())));

        let result = resolve_accesspass_pda(&client, &client_ip, &user_payer);
        assert_eq!(result, dynamic_pda);
    }

    #[test]
    fn test_resolve_accesspass_pda_falls_back_to_static_when_dynamic_absent() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let client_ip: Ipv4Addr = [192, 168, 1, 1].into();
        let user_payer = Pubkey::new_unique();
        let (static_pda, bump) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
        let (dynamic_pda, _) = get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user_payer);

        let ap = make_accesspass(client_ip, user_payer, bump, 0);
        client
            .expect_get()
            .with(predicate::eq(dynamic_pda))
            .returning(|_| Err(eyre::eyre!("not found")));
        client
            .expect_get()
            .with(predicate::eq(static_pda))
            .returning(move |_| Ok(AccountData::AccessPass(ap.clone())));

        let result = resolve_accesspass_pda(&client, &client_ip, &user_payer);
        assert_eq!(result, static_pda);
    }

    #[test]
    fn test_resolve_accesspass_pda_returns_static_when_neither_found() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let client_ip: Ipv4Addr = [192, 168, 1, 1].into();
        let user_payer = Pubkey::new_unique();
        let (static_pda, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

        client
            .expect_get()
            .returning(|_| Err(eyre::eyre!("not found")));

        let result = resolve_accesspass_pda(&client, &client_ip, &user_payer);
        assert_eq!(result, static_pda);
    }
}
