use std::io::Write;

use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::passport::{
    instruction::AccessMode,
    state::{AccessRequest, ProgramConfig},
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;

use crate::{
    error::{PassportCliError, Result},
    output::{emit_json, is_json},
};

#[derive(Debug, Args)]
pub struct FetchArgs {
    #[arg(long)]
    pub config: bool,

    #[arg(long, value_name = "DOUBLEZERO_PUBKEY")]
    pub access_request: Option<Pubkey>,
}

#[derive(Serialize)]
struct ProgramConfigView {
    program_config: String,
    is_paused: bool,
    is_request_access_paused: bool,
    admin_key: String,
    sentinel_key: String,
    request_deposit_sol: f64,
    request_fee_sol: f64,
    solana_validator_backup_ids_limit: u64,
}

#[derive(Serialize)]
struct AccessRequestView {
    access_request: String,
    service_key: String,
    rent_beneficiary_key: String,
    request_fee_sol: f64,
    access_mode: String,
}

/// Combined JSON document emitted when both `--config` and `--access-request`
/// are requested, so the output is a single valid object rather than two
/// back-to-back ones (which would choke `jq`).
#[derive(Serialize)]
struct FetchView {
    program_config: ProgramConfigView,
    access_request: AccessRequestView,
}

fn access_mode_label(access_request: &AccessRequest) -> &'static str {
    match access_request.checked_access_mode() {
        Some(AccessMode::SolanaValidator(_)) => "Solana validator",
        Some(AccessMode::SolanaValidatorWithBackupIds { .. }) => "Solana validator with backup IDs",
        None => "Unknown",
    }
}

impl FetchArgs {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        tracing::debug!(env = %ctx.env, "passport fetch");

        let connection = SolanaConnection::new(ctx.solana_l1_rpc_url.clone());
        let format = ctx.output_format;

        if is_json(format) {
            let program_config = if self.config {
                let (program_config_key, program_config) =
                    fetch_program_config(&connection).await?;
                Some(ProgramConfigView {
                    program_config: program_config_key.to_string(),
                    is_paused: program_config.is_paused(),
                    is_request_access_paused: program_config.is_request_access_paused(),
                    admin_key: program_config.admin_key.to_string(),
                    sentinel_key: program_config.sentinel_key.to_string(),
                    request_deposit_sol: program_config.request_deposit_lamports as f64 * 1e-9,
                    request_fee_sol: program_config.request_fee_lamports as f64 * 1e-9,
                    solana_validator_backup_ids_limit: program_config
                        .solana_validator_backup_ids_limit
                        as u64,
                })
            } else {
                None
            };

            let access_request = if let Some(service_key) = self.access_request {
                let (access_request_key, access_request) =
                    fetch_access_request(&connection, &service_key).await?;
                Some(AccessRequestView {
                    access_request: access_request_key.to_string(),
                    service_key: access_request.service_key.to_string(),
                    rent_beneficiary_key: access_request.rent_beneficiary_key.to_string(),
                    request_fee_sol: access_request.request_fee_lamports as f64 * 1e-9,
                    access_mode: access_mode_label(&access_request).to_string(),
                })
            } else {
                None
            };

            match (program_config, access_request) {
                (Some(program_config), Some(access_request)) => emit_json(
                    out,
                    &FetchView {
                        program_config,
                        access_request,
                    },
                    format,
                )?,
                (Some(program_config), None) => emit_json(out, &program_config, format)?,
                (None, Some(access_request)) => emit_json(out, &access_request, format)?,
                (None, None) => {}
            }

            return Ok(());
        }

        // Human-readable path: reproduces the exact pre-RFC-20 output.
        if self.config {
            let (program_config_key, program_config) = fetch_program_config(&connection).await?;
            write_program_config_human(out, &program_config_key, &program_config)?;
        }

        // NOTE: If an access request is found, the sentinel is not doing its job.
        if let Some(access_request) = self.access_request {
            let (access_request_key, access_request) =
                fetch_access_request(&connection, &access_request).await?;
            write_access_request_human(out, &access_request_key, &access_request)?;
        }

        Ok(())
    }
}

/// Render the program-config pipe-table exactly as the pre-RFC-20 CLI did.
fn write_program_config_human<W: Write>(
    out: &mut W,
    program_config_key: &Pubkey,
    program_config: &ProgramConfig,
) -> Result<()> {
    writeln!(out, "Program config: {program_config_key}")?;
    writeln!(out)?;
    writeln!(out, "Parameter                         | Value")?;
    writeln!(
        out,
        "----------------------------------+-------------------------------------------------"
    )?;
    writeln!(
        out,
        "Is program paused?                | {}",
        program_config.is_paused()
    )?;
    writeln!(
        out,
        "Is request access paused?         | {}",
        program_config.is_request_access_paused()
    )?;
    writeln!(
        out,
        "Admin key                         | {}",
        program_config.admin_key
    )?;
    writeln!(
        out,
        "Sentinel key                      | {}",
        program_config.sentinel_key
    )?;
    writeln!(
        out,
        "Request deposit                   | {:.9} SOL",
        program_config.request_deposit_lamports as f64 * 1e-9
    )?;
    writeln!(
        out,
        "Request fee                       | {:.9} SOL",
        program_config.request_fee_lamports as f64 * 1e-9
    )?;
    writeln!(
        out,
        "Solana validator backup IDs limit | {}",
        program_config.solana_validator_backup_ids_limit
    )?;
    writeln!(out)?;
    Ok(())
}

/// Render the access-request pipe-table exactly as the pre-RFC-20 CLI did.
fn write_access_request_human<W: Write>(
    out: &mut W,
    access_request_key: &Pubkey,
    access_request: &AccessRequest,
) -> Result<()> {
    let access_mode_str = access_mode_label(access_request);

    writeln!(out, "Access request: {access_request_key}")?;
    writeln!(out)?;
    writeln!(out, "Field                | Value")?;
    writeln!(
        out,
        "---------------------+-------------------------------------------------"
    )?;
    writeln!(out, "Service key          | {}", access_request.service_key)?;
    writeln!(
        out,
        "Rent beneficiary key | {}",
        access_request.rent_beneficiary_key
    )?;
    writeln!(
        out,
        "Request fee          | {:.9} SOL",
        access_request.request_fee_lamports as f64 * 1e-9
    )?;
    writeln!(out, "Access mode          | {access_mode_str}")?;
    writeln!(out)?;
    Ok(())
}

async fn fetch_program_config(connection: &SolanaConnection) -> Result<(Pubkey, ProgramConfig)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config = connection
        .try_fetch_zero_copy_data(&program_config_key)
        .await
        .map_err(PassportCliError::other)?;
    Ok((program_config_key, *program_config))
}

async fn fetch_access_request(
    connection: &SolanaConnection,
    service_key: &Pubkey,
) -> Result<(Pubkey, AccessRequest)> {
    let (access_request_key, _) = AccessRequest::find_address(service_key);

    let access_request = connection
        .try_fetch_zero_copy_data(&access_request_key)
        .await
        .map_err(|e| PassportCliError::AccessRequestNotFound {
            service_key: *service_key,
            source: e.into(),
        })?;

    Ok((access_request_key, *access_request.mucked_data))
}

#[cfg(test)]
mod tests {
    use solana_sdk::pubkey::Pubkey;

    use super::*;

    #[test]
    fn program_config_human_output_matches_legacy_layout() {
        let key = Pubkey::new_from_array([7u8; 32]);
        let admin = Pubkey::new_from_array([1u8; 32]);
        let sentinel = Pubkey::new_from_array([2u8; 32]);

        // `ProgramConfig` has private padding fields, so build from `default()`
        // and assign the public fields rather than using struct-update syntax.
        let mut program_config = ProgramConfig::default();
        program_config.admin_key = admin;
        program_config.sentinel_key = sentinel;
        program_config.request_deposit_lamports = 1_500_000_000; // 1.5 SOL
        program_config.request_fee_lamports = 250_000; // 0.00025 SOL
        program_config.solana_validator_backup_ids_limit = 8;
        program_config.set_is_paused(false);
        program_config.set_is_request_access_paused(true);

        let mut out = Vec::new();
        write_program_config_human(&mut out, &key, &program_config).unwrap();
        let rendered = String::from_utf8(out).unwrap();

        // Golden layout: exact column widths, separators, SOL precision, and the
        // blank lines that bracket the table. Pubkeys are interpolated so the
        // assertion pins the layout rather than specific base58 strings.
        let expected = format!(
            "Program config: {key}\n\
\n\
Parameter                         | Value\n\
----------------------------------+-------------------------------------------------\n\
Is program paused?                | false\n\
Is request access paused?         | true\n\
Admin key                         | {admin}\n\
Sentinel key                      | {sentinel}\n\
Request deposit                   | 1.500000000 SOL\n\
Request fee                       | 0.000250000 SOL\n\
Solana validator backup IDs limit | 8\n\
\n"
        );
        assert_eq!(rendered, expected);
    }

    #[test]
    fn request_fee_keeps_nine_decimal_precision() {
        let key = Pubkey::new_from_array([7u8; 32]);
        let mut program_config = ProgramConfig::default();
        program_config.request_fee_lamports = 1; // 0.000000001 SOL

        let mut out = Vec::new();
        write_program_config_human(&mut out, &key, &program_config).unwrap();
        let rendered = String::from_utf8(out).unwrap();

        assert!(
            rendered.contains("Request fee                       | 0.000000001 SOL"),
            "9-decimal SOL precision must be preserved, got:\n{rendered}"
        );
    }

    #[test]
    fn access_request_human_output_matches_legacy_layout() {
        let key = Pubkey::new_from_array([7u8; 32]);
        let service = Pubkey::new_from_array([3u8; 32]);
        let rent = Pubkey::new_from_array([4u8; 32]);

        let access_request = AccessRequest {
            service_key: service,
            rent_beneficiary_key: rent,
            request_fee_lamports: 250_000, // 0.00025 SOL
            ..Default::default()
        };

        let mut out = Vec::new();
        write_access_request_human(&mut out, &key, &access_request).unwrap();
        let rendered = String::from_utf8(out).unwrap();

        // A zeroed encoded access mode decodes as the first borsh variant
        // (`SolanaValidator` with a zeroed attestation), so the label reads
        // "Solana validator".
        let expected = format!(
            "Access request: {key}\n\
\n\
Field                | Value\n\
---------------------+-------------------------------------------------\n\
Service key          | {service}\n\
Rent beneficiary key | {rent}\n\
Request fee          | 0.000250000 SOL\n\
Access mode          | Solana validator\n\
\n"
        );
        assert_eq!(rendered, expected);
    }
}
