use crate::{geoclicommand::GeoCliCommand, validators::validate_code};
use clap::{Args, ValueEnum};
use doublezero_geolocation::state::geolocation_user::GeolocationPaymentStatus;
use doublezero_sdk::geolocation::geolocation_user::update_payment_status::UpdatePaymentStatusCommand;
use std::io::Write;

#[derive(ValueEnum, Debug, Clone)]
pub enum PaymentStatus {
    Paid,
    Delinquent,
}

#[derive(Args, Debug)]
pub struct UpdatePaymentStatusCliCommand {
    /// User code
    #[arg(long, value_parser = validate_code)]
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

        let serviceability_globalstate_pk = client.get_serviceability_globalstate_pk();

        let sig = client.update_payment_status(UpdatePaymentStatusCommand {
            code: self.user,
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
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_update_payment_status_paid() {
        let mut client = MockGeoCliCommand::new();

        let svc_gs_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let signature = Signature::new_unique();

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
