use crate::{geoclicommand::GeoCliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::geolocation::geolocation_user::{
    get::GetGeolocationUserCommand, update::UpdateGeolocationUserCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateGeolocationUserCliCommand {
    /// User pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub user: String,
    /// New payment token account
    #[arg(long)]
    pub token_account: Pubkey,
}

impl UpdateGeolocationUserCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, resolved_user) = client.get_geolocation_user(GetGeolocationUserCommand {
            pubkey_or_code: self.user,
        })?;

        let sig = client.update_geolocation_user(UpdateGeolocationUserCommand {
            code: resolved_user.code,
            token_account: Some(self.token_account),
        })?;

        writeln!(out, "Signature: {sig}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{
        accounttype::AccountType,
        geolocation_user::{
            FlatPerEpochConfig, GeolocationBillingConfig, GeolocationPaymentStatus,
            GeolocationUser, GeolocationUserStatus,
        },
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    fn make_user(token_account: Pubkey) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: "geo-user-01".to_string(),
            token_account,
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets: vec![],
            result_destination: String::new(),
        }
    }

    #[test]
    fn test_cli_update_geolocation_user() {
        let mut client = MockGeoCliCommand::new();

        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let new_token = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::new_unique();

        let user = make_user(Pubkey::new_unique());

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: "geo-user-01".to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        client
            .expect_update_geolocation_user()
            .with(predicate::eq(UpdateGeolocationUserCommand {
                code: "geo-user-01".to_string(),
                token_account: Some(new_token),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = UpdateGeolocationUserCliCommand {
            user: "geo-user-01".to_string(),
            token_account: new_token,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
