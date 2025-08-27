use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::{Args, ValueEnum};
use doublezero_sdk::commands::accesspass::set::SetAccessPassCommand;
use doublezero_serviceability::state::accesspass::AccessPassType;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
pub enum CliAccessPassType {
    Prepaid,
    SolanaValidator,
}

#[derive(Args, Debug)]
pub struct SetAccessPassCliCommand {
    /// Specifies the type of access pass being set [prepaid|postpaid].
    #[arg(long, default_value = "prepaid")]
    pub accesspass_type: CliAccessPassType,
    /// Client IP address in IPv4 format
    #[arg(long)]
    pub client_ip: Ipv4Addr,
    /// Specifies the payer of the access pass.
    #[arg(long)]
    pub user_payer: String,
    /// Specifies the number of epochs for the access pass.
    #[arg(long, default_value = "max")]
    pub epochs: String,
    /// Specifies the Solana validator for the access pass.
    #[arg(long)]
    pub solana_validator: Option<Pubkey>,
}

impl SetAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let user_payer = {
            if self.user_payer.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.user_payer)?
            }
        };

        let current_epoch = client.get_epoch()?;
        let last_access_epoch = match self.epochs.to_ascii_lowercase().as_str() {
            "0" => 0,
            "max" => u64::MAX,
            _ => current_epoch + self.epochs.parse::<u64>()?,
        };

        let accesspass_type = match self.accesspass_type {
            CliAccessPassType::Prepaid => AccessPassType::Prepaid,
            CliAccessPassType::SolanaValidator => match self.solana_validator {
                Some(solana_validator) => AccessPassType::SolanaValidator(solana_validator),
                None => eyre::bail!(
                    "Solana validator access pass type requires --solana-validator <PUBKEY>"
                ),
            },
        };

        let signature = client.set_accesspass(SetAccessPassCommand {
            accesspass_type,
            client_ip: self.client_ip,
            user_payer,
            last_access_epoch,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        accesspass::set::SetAccessPassCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::accesspass::set::SetAccessPassCommand;
    use doublezero_serviceability::{pda::get_accesspass_pda, state::accesspass::AccessPassType};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    use super::*;

    #[test]
    fn test_cli_device_create() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client.expect_get_epoch().returning(|| Ok(10));

        let solana_validator = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::SolanaValidator(solana_validator),
                client_ip,
                user_payer: payer,
                last_access_epoch: 11,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaValidator,
            client_ip,
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Solana validator access pass type requires --solana-validator <PUBKEY>"
        );

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaValidator,
            client_ip,
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: Some(solana_validator),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
