use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::{Args, ValueEnum};
use doublezero_sdk::commands::accesspass::set::SetAccessPassCommand;
use doublezero_serviceability::{pda::get_accesspass_pda, state::accesspass::AccessPassType};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
pub enum CliAccessPassType {
    Prepaid,
    SolanaValidator,
    SolanaRPC,
    SolanaMulticastPublisher,
    SolanaMulticastSubscriber,
    Others,
}

#[derive(Args, Debug)]
pub struct SetAccessPassCliCommand {
    /// Specifies the access pass type (prepaid, solana_validator, solana_rpc, solana_multicast_publisher, solana_multicast_subscriber)
    #[arg(long, default_value = "prepaid")]
    pub accesspass_type: CliAccessPassType,
    /// Client IP address in IPv4 format
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// Specifies the payer of the access pass.
    #[arg(long)]
    pub user_payer: String,
    /// Specifies the number of epochs for the access pass.
    #[arg(long, default_value = "max")]
    pub epochs: String,
    /// Specifies the solana validator node id for the access pass. Required if accesspass_type is solana_validator, solana_rpc, solana_multicast_publisher, or solana_multicast_subscriber
    #[arg(
        long,
        required_if_eq("accesspass_type", "solana_validator"),
        required_if_eq("accesspass_type", "solana_rpc"),
        required_if_eq("accesspass_type", "solana_multicast_publisher"),
        required_if_eq("accesspass_type", "solana_multicast_subscriber")
    )]
    pub solana_validator: Option<Pubkey>, // This will be integrated with identity for all access pass types in future
    /// Allow multiple IP addresses for this access pass (only for Prepaid type)    
    #[arg(long, default_value_t = false)]
    pub allow_multiple_ip: bool,
    /// Specifies the name for other access pass types. Required if accesspass_type is others.
    #[arg(long, required_if_eq("accesspass_type", "others"))]
    pub others_name: Option<String>,
    /// Specifies the key for other access pass types. Required if accesspass_type is others.
    #[arg(long, required_if_eq("accesspass_type", "others"))]
    pub others_key: Option<String>,
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
            CliAccessPassType::SolanaRPC => match self.solana_validator {
                Some(solana_validator) => AccessPassType::SolanaRPC(solana_validator),
                None => {
                    eyre::bail!("Solana RPC access pass type requires --solana-validator <STRING>")
                }
            },
            CliAccessPassType::SolanaMulticastPublisher => match self.solana_validator {
                Some(solana_validator) => {
                    AccessPassType::SolanaMulticastPublisher(solana_validator)
                }
                None => eyre::bail!(
                    "Solana Multicast Publisher access pass type requires --solana-validator <STRING>"
                ),
            },
            CliAccessPassType::SolanaMulticastSubscriber => match self.solana_validator {
                Some(solana_validator) => {
                    AccessPassType::SolanaMulticastSubscriber(solana_validator)
                }
                None => eyre::bail!(
                    "Solana Multicast Subscriber access pass type requires --solana-validator <STRING>"
                ),
            },
            CliAccessPassType::Others => match (self.others_name, self.others_key) {
                (Some(name), Some(key)) => AccessPassType::Others( name, key ),
                _ => eyre::bail!(
                    "Others access pass type requires --others-name <STRING> and --others-key <STRING>"
                ),
            },
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &self.client_ip.unwrap_or(Ipv4Addr::UNSPECIFIED),
            &user_payer,
        );
        writeln!(out, "AccessPass PDA: {accesspass_pubkey}")?;

        let signature = client.set_accesspass(SetAccessPassCommand {
            accesspass_type,
            client_ip: self.client_ip.unwrap_or(Ipv4Addr::UNSPECIFIED),
            user_payer,
            last_access_epoch,
            allow_multiple_ip: self.allow_multiple_ip,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
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
    fn test_cli_accesspass_set_prepaid() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::Prepaid,
                client_ip,
                user_payer: payer,
                last_access_epoch: u64::MAX,
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::Prepaid,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "max".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_validator_missing_validator() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaValidator,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Solana validator access pass type requires --solana-validator <PUBKEY>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_validator_success() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let solana_validator = Pubkey::new_unique();

        client.expect_get_epoch().returning(|| Ok(10));
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
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaValidator,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: Some(solana_validator),
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_rpc_missing_validator() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaRPC,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Solana RPC access pass type requires --solana-validator <STRING>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_rpc_success() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let solana_validator = Pubkey::new_unique();

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::SolanaRPC(solana_validator),
                client_ip,
                user_payer: payer,
                last_access_epoch: 11,
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaRPC,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: Some(solana_validator),
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_multicast_publisher_missing_validator() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaMulticastPublisher,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Solana Multicast Publisher access pass type requires --solana-validator <STRING>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_multicast_publisher_success() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let solana_validator = Pubkey::new_unique();

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::SolanaMulticastPublisher(solana_validator),
                client_ip,
                user_payer: payer,
                last_access_epoch: 11,
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaMulticastPublisher,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: Some(solana_validator),
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_multicast_subscriber_missing_validator() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaMulticastSubscriber,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Solana Multicast Subscriber access pass type requires --solana-validator <STRING>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_solana_multicast_subscriber_success() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let solana_validator = Pubkey::new_unique();

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::SolanaMulticastSubscriber(solana_validator),
                client_ip,
                user_payer: payer,
                last_access_epoch: 11,
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::SolanaMulticastSubscriber,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: Some(solana_validator),
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_accesspass_set_others_missing_name_and_key() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::Others,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: None,
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Others access pass type requires --others-name <STRING> and --others-key <STRING>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_others_missing_key() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::Others,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: Some("custom-name".to_string()),
            others_key: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert_eq!(
            res.err().unwrap().to_string(),
            "Others access pass type requires --others-name <STRING> and --others-key <STRING>"
        );
    }

    #[test]
    fn test_cli_accesspass_set_others_success() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client.expect_get_epoch().returning(|| Ok(10));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_accesspass()
            .with(predicate::eq(SetAccessPassCommand {
                accesspass_type: AccessPassType::Others(
                    "custom-name".to_string(),
                    "custom-key".to_string(),
                ),
                client_ip,
                user_payer: payer,
                last_access_epoch: 11,
                allow_multiple_ip: false,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetAccessPassCliCommand {
            accesspass_type: CliAccessPassType::Others,
            client_ip: Some(client_ip),
            user_payer: payer.to_string(),
            epochs: "1".into(),
            solana_validator: None,
            allow_multiple_ip: false,
            others_name: Some("custom-name".to_string()),
            others_key: Some("custom-key".to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "AccessPass PDA: 6pw9fvwzjjkkocGuwxhmv1TwHHnYTFjGvV9GKX6nkFMw\nSignature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
