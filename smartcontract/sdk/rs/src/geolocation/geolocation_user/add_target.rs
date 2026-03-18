use doublezero_geolocation::{
    instructions::{AddTargetArgs, GeolocationInstruction},
    pda,
    state::geolocation_user::GeoLocationTargetType,
    validation::{validate_code_length, validate_public_ip},
};
use doublezero_program_common::validate_account_code;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct AddTargetCommand {
    pub code: String,
    pub probe_pk: Pubkey,
    pub target_type: GeoLocationTargetType,
    pub ip_address: Ipv4Addr,
    pub location_offset_port: u16,
    pub target_pk: Pubkey,
}

impl AddTargetCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<Signature> {
        validate_code_length(&self.code)?;
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        match self.target_type {
            GeoLocationTargetType::Outbound => {
                validate_public_ip(&self.ip_address)?;
            }
            GeoLocationTargetType::Inbound => {
                if self.target_pk == Pubkey::default() {
                    return Err(eyre::eyre!(
                        "inbound target requires a non-default target_pk"
                    ));
                }
            }
        }

        let program_id = client.get_program_id();
        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, &code);

        client.execute_transaction(
            GeolocationInstruction::AddTarget(AddTargetArgs {
                target_type: self.target_type,
                ip_address: self.ip_address,
                location_offset_port: self.location_offset_port,
                target_pk: self.target_pk,
            }),
            vec![
                AccountMeta::new(user_pda, false),
                AccountMeta::new(self.probe_pk, false),
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
    fn test_add_target_outbound() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";
        let probe_pk = Pubkey::new_unique();

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::AddTarget(AddTargetArgs {
                    target_type: GeoLocationTargetType::Outbound,
                    ip_address: Ipv4Addr::new(8, 8, 8, 8),
                    location_offset_port: 8923,
                    target_pk: Pubkey::default(),
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pda, false),
                    AccountMeta::new(probe_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = AddTargetCommand {
            code: code.to_string(),
            probe_pk,
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_add_target_inbound() {
        let mut client = MockGeolocationClient::new();

        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "geo-user-01";
        let probe_pk = Pubkey::new_unique();
        let target_pk = Pubkey::new_unique();

        let (user_pda, _) = pda::get_geolocation_user_pda(&program_id, code);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(GeolocationInstruction::AddTarget(AddTargetArgs {
                    target_type: GeoLocationTargetType::Inbound,
                    ip_address: Ipv4Addr::UNSPECIFIED,
                    location_offset_port: 0,
                    target_pk,
                })),
                predicate::eq(vec![
                    AccountMeta::new(user_pda, false),
                    AccountMeta::new(probe_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = AddTargetCommand {
            code: code.to_string(),
            probe_pk,
            target_type: GeoLocationTargetType::Inbound,
            ip_address: Ipv4Addr::UNSPECIFIED,
            location_offset_port: 0,
            target_pk,
        };

        let result = command.execute(&client);
        assert!(result.is_ok());
    }
}
