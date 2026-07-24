use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::accesspass::set_flags::SetAccessPassFlagsCommand;
use doublezero_serviceability::pda::get_accesspass_pda;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

/// Mark an access pass as DZF-locked: the DoubleZero Foundation manages the pass out of band, so
/// automated reconcilers (e.g. the Feed Oracle) must not tear it down. Requires `ACCESS_PASS_ADMIN`.
#[derive(Args, Debug)]
pub struct DzfLockAccessPassCliCommand {
    /// Client IP address in IPv4 format (the pass's `client_ip`; omit for passes keyed on 0.0.0.0)
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// Payer of the access pass ("me" for the current keypair)
    #[arg(long)]
    pub user_payer: String,
}

impl DzfLockAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        set_dzf_locked(client, out, self.client_ip, &self.user_payer, true)
    }
}

/// Clear the DZF-locked mark on an access pass, allowing automated reconcilers to manage it again.
/// Requires `ACCESS_PASS_ADMIN`.
#[derive(Args, Debug)]
pub struct DzfUnlockAccessPassCliCommand {
    /// Client IP address in IPv4 format (the pass's `client_ip`; omit for passes keyed on 0.0.0.0)
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// Payer of the access pass ("me" for the current keypair)
    #[arg(long)]
    pub user_payer: String,
}

impl DzfUnlockAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        set_dzf_locked(client, out, self.client_ip, &self.user_payer, false)
    }
}

/// Shared set/clear of the dzf_locked flag. `client_ip` defaults to 0.0.0.0 (matching the
/// `access-pass set` convention) so passes keyed on the unspecified address can be targeted by
/// omitting `--client-ip`.
fn set_dzf_locked<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    client_ip: Option<Ipv4Addr>,
    user_payer: &str,
    locked: bool,
) -> eyre::Result<()> {
    client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

    let user_payer = if user_payer.eq_ignore_ascii_case("me") {
        client.get_payer()
    } else {
        Pubkey::from_str(user_payer)?
    };
    let client_ip = client_ip.unwrap_or(Ipv4Addr::UNSPECIFIED);

    let (accesspass_pubkey, _) =
        get_accesspass_pda(&client.get_program_id(), &client_ip, &user_payer);
    writeln!(out, "AccessPass PDA: {accesspass_pubkey}")?;

    let signature = client.set_accesspass_flags(SetAccessPassFlagsCommand {
        client_ip,
        user_payer,
        allow_multiple_ip: None,
        dzf_locked: Some(locked),
    })?;
    writeln!(out, "Signature: {signature}")?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use crate::{
        accesspass::dzf_lock::{DzfLockAccessPassCliCommand, DzfUnlockAccessPassCliCommand},
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::commands::accesspass::set_flags::SetAccessPassFlagsCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_accesspass_dzf_lock() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let client_ip = [100, 0, 0, 1].into();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass_flags()
            .with(predicate::eq(SetAccessPassFlagsCommand {
                client_ip,
                user_payer: payer,
                allow_multiple_ip: None,
                dzf_locked: Some(true),
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DzfLockAccessPassCliCommand {
                client_ip: Some(client_ip),
                user_payer: payer.to_string(),
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
    }

    #[test]
    fn test_cli_accesspass_dzf_unlock() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let client_ip = [100, 0, 0, 1].into();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass_flags()
            .with(predicate::eq(SetAccessPassFlagsCommand {
                client_ip,
                user_payer: payer,
                allow_multiple_ip: None,
                dzf_locked: Some(false),
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DzfUnlockAccessPassCliCommand {
                client_ip: Some(client_ip),
                user_payer: payer.to_string(),
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
    }
}
