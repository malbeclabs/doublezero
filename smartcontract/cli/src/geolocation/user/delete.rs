use crate::{geoclicommand::GeoCliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::geolocation::geolocation_user::delete::DeleteGeolocationUserCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteGeolocationUserCliCommand {
    /// User code to delete
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Skip confirmation prompt
    #[arg(long, default_value_t = false)]
    pub yes: bool,
}

impl DeleteGeolocationUserCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        if !self.yes {
            eprint!("Delete user '{}'? [y/N]: ", self.code);
            let mut input = String::new();
            std::io::stdin().read_line(&mut input)?;
            if !input.trim().eq_ignore_ascii_case("y") {
                writeln!(out, "Aborted.")?;
                return Ok(());
            }
        }

        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let sig = client.delete_geolocation_user(DeleteGeolocationUserCommand {
            code: self.code,
            serviceability_globalstate_pk,
        })?;

        writeln!(out, "Signature: {sig}")?;

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
    fn test_cli_geolocation_user_delete() {
        let mut client = MockGeoCliCommand::new();

        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_get_serviceability_globalstate_pk()
            .returning(move || svc_gs_pk);

        client
            .expect_delete_geolocation_user()
            .with(predicate::eq(DeleteGeolocationUserCommand {
                code: "geo-user-01".to_string(),
                serviceability_globalstate_pk: svc_gs_pk,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = DeleteGeolocationUserCliCommand {
            code: "geo-user-01".to_string(),
            yes: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
