use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_parse_bandwidth, validate_pubkey},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::multicastgroup::{
    create::CreateMulticastGroupCommand, get::GetMulticastGroupCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateMulticastGroupCliCommand {
    /// Unique code for the multicast group
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Maximum bandwidth for the group (e.g. 10Gbps, 100Mbps)
    #[arg(long, value_parser = validate_parse_bandwidth)]
    pub max_bandwidth: u64,
    /// Owner Pubkey or 'me' for current payer. Defaults to me with --bond-ref
    #[arg(long, value_parser = validate_pubkey, required_unless_present = "bond_ref")]
    pub owner: Option<String>,
    /// Create the group on this BuilderBond instead of on the multicast admin permission. The
    /// bond must be yours, and the group is owned by you
    #[arg(long)]
    pub bond_ref: Option<Pubkey>,
    /// Wait for the multicast group to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl CreateMulticastGroupCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let owner_pk = match self.owner.as_deref() {
            None => client.get_payer(),
            Some(owner) if owner.eq_ignore_ascii_case("me") => client.get_payer(),
            Some(owner) => Pubkey::from_str(owner)?,
        };
        // The program refuses a group created on a bond unless the payer owns it. Say so here
        // rather than after a transaction fee.
        if self.bond_ref.is_some() && owner_pk != client.get_payer() {
            eyre::bail!(
                "a group created on a bond is owned by you: drop --owner or pass --owner me"
            );
        }

        let (signature, pubkey) = client.create_multicastgroup(CreateMulticastGroupCommand {
            code: self.code.clone(),
            max_bandwidth: self.max_bandwidth,
            owner: owner_pk,
            bond_ref: self.bond_ref.unwrap_or_default(),
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let (_, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
                pubkey_or_code: pubkey.to_string(),
            })?;
            writeln!(out, "Status: {0}", mgroup.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        doublezerocommand::CliCommand,
        multicastgroup::create::CreateMulticastGroupCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::multicastgroup::create::CreateMulticastGroupCommand, get_device_pda,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_multicastgroup_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_create_multicastgroup()
            .with(predicate::eq(CreateMulticastGroupCommand {
                code: "test".to_string(),
                max_bandwidth: 10_000_000_000,
                owner: pda_pubkey,
                bond_ref: Pubkey::default(),
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            CreateMulticastGroupCliCommand {
                code: "test".to_string(),
                max_bandwidth: 10_000_000_000,
                owner: Some(pda_pubkey.to_string()),
                bond_ref: None,
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_multicastgroup_create_on_a_bond() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let bond_ref = Pubkey::new_unique();
        let group = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_create_multicastgroup()
            .with(predicate::eq(CreateMulticastGroupCommand {
                code: "bonded".to_string(),
                max_bandwidth: 1_000_000_000,
                owner: payer,
                bond_ref,
            }))
            .times(1)
            .returning(move |_| Ok((Signature::new_unique(), group)));

        let mut output = Vec::new();
        let res = block_on(
            CreateMulticastGroupCliCommand {
                code: "bonded".to_string(),
                max_bandwidth: 1_000_000_000,
                owner: None,
                bond_ref: Some(bond_ref),
                wait: false,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    #[test]
    fn test_cli_multicastgroup_create_on_a_bond_for_someone_else_fails_early() {
        let mut client = create_test_client();
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client.expect_create_multicastgroup().times(0);

        let mut output = Vec::new();
        let res = block_on(
            CreateMulticastGroupCliCommand {
                code: "bonded".to_string(),
                max_bandwidth: 1_000_000_000,
                owner: Some(Pubkey::new_unique().to_string()),
                bond_ref: Some(Pubkey::new_unique()),
                wait: false,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_err(), "the CLI should refuse before sending");
    }
}
