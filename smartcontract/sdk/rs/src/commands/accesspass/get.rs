use std::net::Ipv4Addr;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    state::{accesspass::AccessPass, accountdata::AccountData},
};
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

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::get::GetAccessPassCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
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
}
