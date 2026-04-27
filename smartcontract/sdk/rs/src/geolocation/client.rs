use crate::{
    config::{
        convert_geo_program_moniker, convert_url_moniker, default_geolocation_program_id,
        read_doublezero_config,
    },
    keypair::load_keypair,
};
use doublezero_geolocation::instructions::GeolocationInstruction;
use eyre::{eyre, OptionExt};
use log::debug;
use mockall::automock;
use solana_client::rpc_client::RpcClient;
use solana_rpc_client_api::config::RpcProgramAccountsConfig;
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentConfig,
    compute_budget::ComputeBudgetInstruction,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    transaction::Transaction,
};
use solana_system_interface::program;
use std::{path::PathBuf, str::FromStr};

/// CU budget for instructions that scan the targets section of a `GeolocationUser`.
/// `AddTarget`, `RemoveTarget`, and `SetResultDestination` all walk every target onchain
/// (duplicate check, find-by-fields, and unique-probe-set check respectively); each
/// scan exhausts the default 200K CU limit well before the program's `MAX_TARGETS = 4096`
/// upper bound (~743 in practice for AddTarget). Sized to match the value validated by
/// `add_target_cu_benchmark.rs`, which is also the per-tx cap.
pub const TARGET_SCAN_COMPUTE_UNIT_LIMIT: u32 = 1_400_000;

/// Returns the CU limit the SDK should request for a given geolocation instruction, or
/// `None` to leave the runtime default (200K) in place. Exhaustive match: adding a new
/// instruction variant forces a decision here.
fn compute_unit_limit_for(instruction: &GeolocationInstruction) -> Option<u32> {
    use GeolocationInstruction::*;
    match instruction {
        // O(n) walk over all targets onchain. See add_target_cu_benchmark.rs.
        AddTarget(_) | RemoveTarget(_) | SetResultDestination(_) => {
            Some(TARGET_SCAN_COMPUTE_UNIT_LIMIT)
        }
        // Cursor-based prefix-only writes; cost is O(1) in target count.
        InitProgramConfig(_)
        | UpdateProgramConfig(_)
        | CreateGeoProbe(_)
        | UpdateGeoProbe(_)
        | DeleteGeoProbe
        | AddParentDevice
        | RemoveParentDevice(_)
        | CreateGeolocationUser(_)
        | UpdateGeolocationUser(_)
        // DeleteGeolocationUser refuses if targets_count != 0, so it never scans.
        | DeleteGeolocationUser
        | UpdatePaymentStatus(_) => None,
    }
}

#[automock]
pub trait GeolocationClient {
    fn get_program_id(&self) -> Pubkey;
    fn get_payer(&self) -> Pubkey;
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account>;
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>>;
    fn execute_transaction(
        &self,
        instruction: GeolocationInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature>;
}

pub struct GeoClient {
    client: RpcClient,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
}

impl GeoClient {
    pub fn new(
        rpc_url: Option<String>,
        program_id: Option<String>,
        keypair: Option<PathBuf>,
    ) -> eyre::Result<GeoClient> {
        let (_, config) = read_doublezero_config()?;

        let rpc_url = convert_url_moniker(rpc_url.unwrap_or(config.json_rpc_url));
        let client = RpcClient::new_with_commitment(rpc_url, CommitmentConfig::confirmed());
        let payer = load_keypair(keypair, None, config.keypair_path)
            .ok()
            .map(|r| r.keypair);

        let program_id = match program_id {
            None => match &config.geo_program_id {
                Some(id) => {
                    let converted = convert_geo_program_moniker(id.clone());
                    Pubkey::from_str(&converted)
                        .map_err(|_| eyre!("Invalid geo_program_id in config"))?
                }
                None => default_geolocation_program_id(),
            },
            Some(pg_id) => {
                let converted_id = convert_geo_program_moniker(pg_id);
                Pubkey::from_str(&converted_id).map_err(|_| eyre!("Invalid program ID"))?
            }
        };

        Ok(GeoClient {
            client,
            payer,
            program_id,
        })
    }
}

impl GeolocationClient for GeoClient {
    fn get_program_id(&self) -> Pubkey {
        self.program_id
    }

    fn get_payer(&self) -> Pubkey {
        self.payer.as_ref().map(|k| k.pubkey()).unwrap_or_default()
    }

    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        self.client.get_account(&pubkey).map_err(|e| eyre!(e))
    }

    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>> {
        self.client
            .get_program_accounts_with_config(program_id, config)
            .map_err(|e| eyre!(e))
    }

    fn execute_transaction(
        &self,
        instruction: GeolocationInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;
        let data = borsh::to_vec(&instruction)
            .map_err(|e| eyre!("failed to serialize instruction: {e}"))?;

        let main_ix = Instruction::new_with_bytes(
            self.program_id,
            &data,
            [
                accounts,
                vec![
                    AccountMeta::new(payer.pubkey(), true),
                    AccountMeta::new(program::id(), false),
                ],
            ]
            .concat(),
        );

        let instructions: Vec<Instruction> = match compute_unit_limit_for(&instruction) {
            Some(limit) => vec![
                ComputeBudgetInstruction::set_compute_unit_limit(limit),
                main_ix,
            ],
            None => vec![main_ix],
        };

        let mut transaction = Transaction::new_with_payer(&instructions, Some(&payer.pubkey()));

        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;
        transaction.sign(&[payer], blockhash);

        debug!("Simulating transaction: {transaction:?}");

        let result = self.client.simulate_transaction(&transaction)?;
        if result.value.err.is_some() {
            eprintln!("Program Logs:");
            if let Some(logs) = result.value.logs {
                for log in logs {
                    eprintln!("{log}");
                }
            }
        }

        if let Some(err) = result.value.err {
            return Err(eyre!(err));
        }

        self.client
            .send_and_confirm_transaction(&transaction)
            .map_err(|e| eyre!(e))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_geolocation::{
        instructions::{
            AddTargetArgs, CreateGeoProbeArgs, CreateGeolocationUserArgs, GeolocationInstruction,
            InitProgramConfigArgs, RemoveParentDeviceArgs, RemoveTargetArgs,
            SetResultDestinationArgs, UpdateGeoProbeArgs, UpdateGeolocationUserArgs,
            UpdatePaymentStatusArgs, UpdateProgramConfigArgs,
        },
        state::geolocation_user::{GeoLocationTargetType, GeolocationPaymentStatus},
    };
    use std::net::Ipv4Addr;

    #[test]
    fn target_scan_instructions_request_max_compute_budget() {
        // AddTarget, RemoveTarget, and SetResultDestination all walk every target in the
        // user account onchain (duplicate check, find-by-fields, and unique-probe-set
        // check respectively). At MAX_TARGETS=4096 the scan exceeds the default 200K CU
        // limit; the SDK bumps every such transaction to the per-tx ceiling (1.4M) so
        // consumers don't have to think about compute budgets up to that bound.
        let target_scan: &[GeolocationInstruction] = &[
            GeolocationInstruction::AddTarget(AddTargetArgs {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 0,
                target_pk: Pubkey::default(),
            }),
            GeolocationInstruction::RemoveTarget(RemoveTargetArgs {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                target_pk: Pubkey::default(),
            }),
            GeolocationInstruction::SetResultDestination(SetResultDestinationArgs {
                destination: String::new(),
            }),
        ];
        for ix in target_scan {
            assert_eq!(
                compute_unit_limit_for(ix),
                Some(TARGET_SCAN_COMPUTE_UNIT_LIMIT),
                "{ix:?}",
            );
        }
    }

    #[test]
    fn non_target_instructions_use_default_compute_budget() {
        // None ⇒ no ComputeBudgetInstruction prepended; the runtime default (200K CU) is
        // sufficient for these. Listed exhaustively so a new variant cannot silently fall
        // into either bucket — the match in compute_unit_limit_for is exhaustive too.
        let no_budget: &[GeolocationInstruction] = &[
            GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
            GeolocationInstruction::UpdateProgramConfig(UpdateProgramConfigArgs {
                version: None,
                min_compatible_version: None,
            }),
            GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
                code: "p".to_string(),
                public_ip: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 0,
                metrics_publisher_pk: Pubkey::default(),
            }),
            GeolocationInstruction::UpdateGeoProbe(UpdateGeoProbeArgs {
                public_ip: None,
                location_offset_port: None,
                metrics_publisher_pk: None,
            }),
            GeolocationInstruction::DeleteGeoProbe,
            GeolocationInstruction::AddParentDevice,
            GeolocationInstruction::RemoveParentDevice(RemoveParentDeviceArgs {
                device_pk: Pubkey::default(),
            }),
            GeolocationInstruction::CreateGeolocationUser(CreateGeolocationUserArgs {
                code: "u".to_string(),
                token_account: Pubkey::default(),
            }),
            GeolocationInstruction::UpdateGeolocationUser(UpdateGeolocationUserArgs {
                token_account: None,
            }),
            GeolocationInstruction::DeleteGeolocationUser,
            GeolocationInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
                payment_status: GeolocationPaymentStatus::Paid,
                last_deduction_dz_epoch: None,
            }),
        ];
        for ix in no_budget {
            assert_eq!(compute_unit_limit_for(ix), None, "{ix:?}");
        }
    }
}
