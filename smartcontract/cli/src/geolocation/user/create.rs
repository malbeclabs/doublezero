use crate::{
    geoclicommand::GeoCliCommand,
    validators::{validate_code, validate_pubkey},
};
use clap::Args;
use doublezero_sdk::geolocation::geolocation_user::create::CreateGeolocationUserCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateGeolocationUserCliCommand {
    /// Unique user code (e.g., "geo-user-01")
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// 2Z token account pubkey for billing
    #[arg(long, value_parser = validate_pubkey)]
    pub token_account: String,
}

impl CreateGeolocationUserCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let token_account: Pubkey = self.token_account.parse().expect("validated by clap");

        let (sig, pda) = client.create_geolocation_user(CreateGeolocationUserCommand {
            code: self.code,
            token_account,
        })?;

        writeln!(out, "Signature: {sig}\nAccount: {pda}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_geolocation_user_create() {
        let mut client = MockGeoCliCommand::new();

        let token_account = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let user_pda = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_create_geolocation_user()
            .with(predicate::eq(CreateGeolocationUserCommand {
                code: "geo-user-01".to_string(),
                token_account,
            }))
            .returning(move |_| Ok((signature, user_pda)));

        let mut output = Vec::new();
        let res = CreateGeolocationUserCliCommand {
            code: "geo-user-01".to_string(),
            token_account: token_account.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
        assert!(output_str.contains(&user_pda.to_string()));
    }
}
