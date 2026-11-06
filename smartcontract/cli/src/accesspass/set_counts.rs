use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::accesspass::set_counts::SetAccessPassCountsCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

/// Overwrite the user counts of an access pass with the users you counted on it. The program sets
/// `connections` to the sum of the two counts. Requires `ACCESS_PASS_ADMIN`.
#[derive(Args, Debug)]
pub struct SetCountsAccessPassCliCommand {
    /// Access pass public key
    #[arg(long)]
    pub pubkey: Pubkey,
    /// Number of unicast users on the pass (IBRL, IBRL with an allocated IP, edge filtering)
    #[arg(long)]
    pub unicast_users: u16,
    /// Number of multicast users on the pass
    #[arg(long)]
    pub multicast_users: u16,
}

impl SetCountsAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let connections = self
            .unicast_users
            .checked_add(self.multicast_users)
            .ok_or_else(|| {
                eyre::eyre!("--unicast-users plus --multicast-users must be 65535 or less")
            })?;

        let accesspass = client
            .get_account_data(self.pubkey)?
            .get_accesspass()
            .map_err(|e| eyre::eyre!("Account {} is not an access pass: {e}", self.pubkey))?;
        writeln!(
            out,
            "Access pass {}: connections {} -> {connections}, unicast users {} -> {}, multicast users {} -> {}",
            self.pubkey,
            accesspass.connection_count,
            accesspass.unicast_user_count,
            self.unicast_users,
            accesspass.multicast_user_count,
            self.multicast_users,
        )?;

        let signature = client.set_accesspass_counts(SetAccessPassCountsCommand {
            pubkey: self.pubkey,
            unicast_user_count: self.unicast_users,
            multicast_user_count: self.multicast_users,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        accesspass::set_counts::SetCountsAccessPassCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::commands::accesspass::set_counts::SetAccessPassCountsCommand;
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accountdata::AccountData,
        accounttype::AccountType,
        user::User,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn stale_accesspass() -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [100, 0, 0, 1].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: u64::MAX,
            connection_count: 2,
            status: AccessPassStatus::Connected,
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
    fn test_cli_access_pass_set_counts() {
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();
        let signature = Signature::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_account_data()
            .with(predicate::eq(pubkey))
            .returning(|_| Ok(AccountData::AccessPass(stale_accesspass())));
        client
            .expect_set_accesspass_counts()
            .with(predicate::eq(SetAccessPassCountsCommand {
                pubkey,
                unicast_user_count: 1,
                multicast_user_count: 0,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = block_on(
            SetCountsAccessPassCliCommand {
                pubkey,
                unicast_users: 1,
                multicast_users: 0,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!(
                "Access pass {pubkey}: connections 2 -> 1, unicast users 0 -> 1, multicast users 0 -> 0\nSignature: {signature}\n"
            )
        );
    }

    #[test]
    fn test_cli_access_pass_set_counts_refuses_an_account_that_is_not_a_pass() {
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_account_data()
            .with(predicate::eq(pubkey))
            .returning(|_| Ok(AccountData::User(User::default())));
        client.expect_set_accesspass_counts().times(0);

        let mut output = Vec::new();
        let err = block_on(
            SetCountsAccessPassCliCommand {
                pubkey,
                unicast_users: 0,
                multicast_users: 0,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        )
        .expect_err("a non-pass account must be refused");
        assert!(err.to_string().contains("is not an access pass"), "{err}");
    }

    #[test]
    fn test_cli_access_pass_set_counts_refuses_a_sum_over_u16() {
        let mut client = create_test_client();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client.expect_get_account_data().times(0);
        client.expect_set_accesspass_counts().times(0);

        let mut output = Vec::new();
        let err = block_on(
            SetCountsAccessPassCliCommand {
                pubkey: Pubkey::new_unique(),
                unicast_users: u16::MAX,
                multicast_users: 1,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        )
        .expect_err("a sum over u16 must be refused");
        assert!(err.to_string().contains("65535 or less"), "{err}");
    }
}
