use clap::Args;
use doublezero_sdk::commands::globalstate::setinternetlatencycollector::SetInternetLatencyCollectorCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_FOUNDATION_ALLOWLIST, CHECK_ID_JSON},
};

#[derive(Args, Debug)]
pub struct SetInternetLatencyCollectorCliCommand {
    /// Foundation Pubkey to set as the Internet Latency Samples collector
    #[arg(long)]
    pub pubkey: String,
}

impl SetInternetLatencyCollectorCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_FOUNDATION_ALLOWLIST)?;

        let pubkey = {
            if self.pubkey.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.pubkey)?
            }
        };

        let signature =
            client.set_internet_latency_collector(SetInternetLatencyCollectorCommand { pubkey })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_set_internet_latency_collector() {
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_FOUNDATION_ALLOWLIST))
            .returning(|_| Ok(()));
        client
            .expect_set_internet_latency_collector()
            .with(predicate::eq(SetInternetLatencyCollectorCommand { pubkey }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = SetInternetLatencyCollectorCliCommand {
            pubkey: pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n");
    }
}
