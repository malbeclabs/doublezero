use crate::{geoclicommand::GeoCliCommand, validators::validate_pubkey_or_code};
use clap::{Args, ValueEnum};
use doublezero_geolocation::state::geolocation_user::GeolocationPaymentStatus;
use doublezero_sdk::geolocation::geolocation_user::{
    get::GetGeolocationUserCommand, update_payment_status::UpdatePaymentStatusCommand,
};
use std::io::Write;

#[derive(ValueEnum, Debug, Clone)]
pub enum PaymentStatus {
    Paid,
    Delinquent,
}

#[derive(Args, Debug)]
pub struct UpdatePaymentStatusCliCommand {
    /// User code or pubkey
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub user: String,
    /// New payment status
    #[arg(long, value_enum)]
    pub status: PaymentStatus,
    /// Last deduction DZ epoch (optional)
    #[arg(long)]
    pub last_deduction_epoch: Option<u64>,
}

impl UpdatePaymentStatusCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let payment_status = match self.status {
            PaymentStatus::Paid => GeolocationPaymentStatus::Paid,
            PaymentStatus::Delinquent => GeolocationPaymentStatus::Delinquent,
        };

        let (_, resolved_user) = client.get_geolocation_user(GetGeolocationUserCommand {
            pubkey_or_code: self.user,
        })?;

        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let sig = client.update_payment_status(UpdatePaymentStatusCommand {
            code: resolved_user.code,
            serviceability_globalstate_pk,
            payment_status,
            last_deduction_dz_epoch: self.last_deduction_epoch,
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
            FlatPerEpochConfig, GeolocationBillingConfig, GeolocationUser, GeolocationUserStatus,
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn mock_get_geolocation_user(client: &mut MockGeoCliCommand) {
        client.expect_get_geolocation_user().returning(move |cmd| {
            Ok((
                Pubkey::new_unique(),
                GeolocationUser {
                    account_type: AccountType::GeolocationUser,
                    owner: Pubkey::new_unique(),
                    code: cmd.pubkey_or_code.clone(),
                    token_account: Pubkey::new_unique(),
                    payment_status: GeolocationPaymentStatus::Paid,
                    billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                        rate: 1000,
                        last_deduction_dz_epoch: 42,
                    }),
                    status: GeolocationUserStatus::Activated,
                    targets: vec![],
                    result_destination: String::new(),
                },
            ))
        });
    }

    #[test]
    fn test_cli_update_payment_status_paid() {
        let mut client = MockGeoCliCommand::new();

        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::new_unique();

        mock_get_geolocation_user(&mut client);

        client
            .expect_get_serviceability_globalstate_pk()
            .returning(move || svc_gs_pk);

        client
            .expect_update_payment_status()
            .with(predicate::eq(UpdatePaymentStatusCommand {
                code: "geo-user-01".to_string(),
                serviceability_globalstate_pk: svc_gs_pk,
                payment_status: GeolocationPaymentStatus::Paid,
                last_deduction_dz_epoch: Some(42),
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = UpdatePaymentStatusCliCommand {
            user: "geo-user-01".to_string(),
            status: PaymentStatus::Paid,
            last_deduction_epoch: Some(42),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_update_payment_status_delinquent() {
        let mut client = MockGeoCliCommand::new();

        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::new_unique();

        mock_get_geolocation_user(&mut client);

        client
            .expect_get_serviceability_globalstate_pk()
            .returning(move || svc_gs_pk);

        client
            .expect_update_payment_status()
            .with(predicate::eq(UpdatePaymentStatusCommand {
                code: "geo-user-01".to_string(),
                serviceability_globalstate_pk: svc_gs_pk,
                payment_status: GeolocationPaymentStatus::Delinquent,
                last_deduction_dz_epoch: None,
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = UpdatePaymentStatusCliCommand {
            user: "geo-user-01".to_string(),
            status: PaymentStatus::Delinquent,
            last_deduction_epoch: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }
}
