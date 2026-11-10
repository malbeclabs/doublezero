use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::accesspass::set_counts::SetAccessPassCountsCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

/// Replace the user counts of an access pass with the users you counted on it. The program sets
/// `connections` to the sum of the three counts. Requires `ACCESS_PASS_ADMIN`.
///
/// The `--expected-*` flags are the counts the pass held when you counted its users. The command
/// fails, and changes nothing, when the pass no longer holds them: a user connected or
/// disconnected in between, so count again.
#[derive(Args, Debug)]
pub struct SetCountsAccessPassCliCommand {
    /// Access pass public key
    #[arg(long)]
    pub pubkey: Pubkey,
    /// Connection count the pass holds now
    #[arg(long)]
    pub expected_connections: u16,
    /// Unicast user count the pass holds now
    #[arg(long)]
    pub expected_unicast_users: u16,
    /// Public multicast user count the pass holds now
    #[arg(long)]
    pub expected_multicast_users: u16,
    /// Number of unicast users on the pass (IBRL, IBRL with an allocated IP, edge filtering)
    #[arg(long)]
    pub unicast_users: u16,
    /// Number of multicast users on the pass that hold a pass seat
    #[arg(long)]
    pub multicast_users: u16,
    /// Number of multicast users on the pass that hold no pass seat
    #[arg(long)]
    pub private_multicast_users: u16,
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
            .and_then(|count| count.checked_add(self.private_multicast_users))
            .ok_or_else(|| {
                eyre::eyre!("--unicast-users plus --multicast-users plus --private-multicast-users must be 65535 or less")
            })?;

        let accesspass = client
            .get_account_data(self.pubkey)?
            .get_accesspass()
            .map_err(|e| eyre::eyre!("Account {} is not an access pass: {e}", self.pubkey))?;
        // The program checks this too. Checking here first names the counts that changed
        // without sending a transaction that is sure to fail.
        let current = (
            accesspass.connection_count,
            accesspass.unicast_user_count,
            accesspass.public_multicast_user_count,
        );
        let expected = (
            self.expected_connections,
            self.expected_unicast_users,
            self.expected_multicast_users,
        );
        if current != expected {
            eyre::bail!(
                "Access pass {} has connections {}, unicast users {}, multicast users {}, not the expected {}, {}, {}. A user connected or disconnected since the count, so count again.",
                self.pubkey,
                current.0,
                current.1,
                current.2,
                expected.0,
                expected.1,
                expected.2,
            );
        }
        writeln!(
            out,
            "Access pass {}: connections {} -> {connections}, unicast users {} -> {}, multicast users {} -> {}, private multicast users {}",
            self.pubkey,
            accesspass.connection_count,
            accesspass.unicast_user_count,
            self.unicast_users,
            accesspass.public_multicast_user_count,
            self.multicast_users,
            self.private_multicast_users,
        )?;

        let signature = client.set_accesspass_counts(SetAccessPassCountsCommand {
            pubkey: self.pubkey,
            expected_connection_count: self.expected_connections,
            expected_unicast_user_count: self.expected_unicast_users,
            expected_public_multicast_user_count: self.expected_multicast_users,
            unicast_user_count: self.unicast_users,
            public_multicast_user_count: self.multicast_users,
            private_multicast_user_count: self.private_multicast_users,
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
            public_multicast_user_count: 0,
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
                expected_connection_count: 2,
                expected_unicast_user_count: 0,
                expected_public_multicast_user_count: 0,
                unicast_user_count: 1,
                public_multicast_user_count: 0,
                private_multicast_user_count: 1,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = block_on(
            SetCountsAccessPassCliCommand {
                pubkey,
                expected_connections: 2,
                expected_unicast_users: 0,
                expected_multicast_users: 0,
                unicast_users: 1,
                multicast_users: 0,
                private_multicast_users: 1,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!(
                "Access pass {pubkey}: connections 2 -> 2, unicast users 0 -> 1, multicast users 0 -> 0, private multicast users 1\nSignature: {signature}\n"
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
                expected_connections: 0,
                expected_unicast_users: 0,
                expected_multicast_users: 0,
                unicast_users: 0,
                multicast_users: 0,
                private_multicast_users: 0,
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
                expected_connections: 0,
                expected_unicast_users: 0,
                expected_multicast_users: 0,
                unicast_users: u16::MAX - 1,
                multicast_users: 0,
                private_multicast_users: 2,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        )
        .expect_err("a sum over u16 must be refused");
        assert!(err.to_string().contains("65535 or less"), "{err}");
    }

    #[test]
    fn test_cli_access_pass_set_counts_refuses_stale_expected_counts() {
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_account_data()
            .with(predicate::eq(pubkey))
            .returning(|_| Ok(AccountData::AccessPass(stale_accesspass())));
        client.expect_set_accesspass_counts().times(0);

        let mut output = Vec::new();
        let err = block_on(
            SetCountsAccessPassCliCommand {
                pubkey,
                expected_connections: 1,
                expected_unicast_users: 0,
                expected_multicast_users: 0,
                unicast_users: 0,
                multicast_users: 0,
                private_multicast_users: 0,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        )
        .expect_err("stale expected counts must be refused");
        assert_eq!(
            err.to_string(),
            format!(
                "Access pass {pubkey} has connections 2, unicast users 0, multicast users 0, not the expected 1, 0, 0. A user connected or disconnected since the count, so count again."
            )
        );
        assert!(output.is_empty());
    }
}
