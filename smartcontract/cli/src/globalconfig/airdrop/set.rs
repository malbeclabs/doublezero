use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::globalstate::setairdrop::SetAirdropCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetAirdropCliCommand {
    /// New contributor airdrop amount
    #[arg(long)]
    pub contributor_airdrop_lamports: Option<u64>,

    /// New user airdrop amount
    #[arg(long)]
    pub user_airdrop_lamports: Option<u64>,
}

impl SetAirdropCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.set_airdrop(SetAirdropCommand {
            contributor_airdrop_lamports: self.contributor_airdrop_lamports,
            user_airdrop_lamports: self.user_airdrop_lamports,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::airdrop::set::SetAirdropCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::globalstate::setairdrop::SetAirdropCommand;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_globalconfig_set() {
        let mut client = create_test_client();

        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let contributor_airdrop_lamports = 1_000_000_000;
        let user_airdrop_lamports = 40_000;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_airdrop()
            .with(predicate::eq(SetAirdropCommand {
                contributor_airdrop_lamports: Some(contributor_airdrop_lamports),
                user_airdrop_lamports: Some(user_airdrop_lamports),
            }))
            .returning(move |_| Ok(signature));

        // Set all global config; reflects initializing globla config or updating all values
        let mut output = Vec::new();
        let res = SetAirdropCliCommand {
            contributor_airdrop_lamports: Some(1_000_000_000),
            user_airdrop_lamports: Some(40_000),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n" );
    }
}
