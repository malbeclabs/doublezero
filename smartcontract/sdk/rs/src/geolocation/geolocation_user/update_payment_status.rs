use doublezero_geolocation::{
    instructions::{GeolocationInstruction, UpdatePaymentStatusArgs},
    pda,
    state::geolocation_user::GeolocationPaymentStatus,
    validation::validate_code_length,
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdatePaymentStatusCommand {
    pub code: String,
    pub serviceability_globalstate_pk: Pubkey,
    pub payment_status: GeolocationPaymentStatus,
    pub last_deduction_dz_epoch: Option<u64>,
}

impl UpdatePaymentStatusCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, &code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client.execute_transaction(
            GeolocationInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
                payment_status: self.payment_status,
                last_deduction_dz_epoch: self.last_deduction_dz_epoch,
            }),
            vec![
                AccountMeta::new(user_pda, false),
                AccountMeta::new_readonly(config_pda, false),
                AccountMeta::new_readonly(self.serviceability_globalstate_pk, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use mockall::predicate;

    #[test]
    fn test_update_payment_status_paid() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let svc_gs = Pubkey::new_unique();
        let code = "geo-user-01";

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::UpdatePaymentStatus(
                    UpdatePaymentStatusArgs {
                        payment_status: GeolocationPaymentStatus::Paid,
                        last_deduction_dz_epoch: Some(42),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pda, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(svc_gs, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = UpdatePaymentStatusCommand {
            code: code.to_string(),
            serviceability_globalstate_pk: svc_gs,
            payment_status: GeolocationPaymentStatus::Paid,
            last_deduction_dz_epoch: Some(42),
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_update_payment_status_delinquent() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let svc_gs = Pubkey::new_unique();
        let code = "geo-user-01";

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::UpdatePaymentStatus(
                    UpdatePaymentStatusArgs {
                        payment_status: GeolocationPaymentStatus::Delinquent,
                        last_deduction_dz_epoch: None,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pda, false),
                    AccountMeta::new_readonly(config_pda, false),
                    AccountMeta::new_readonly(svc_gs, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = UpdatePaymentStatusCommand {
            code: code.to_string(),
            serviceability_globalstate_pk: svc_gs,
            payment_status: GeolocationPaymentStatus::Delinquent,
            last_deduction_dz_epoch: None,
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }
}
