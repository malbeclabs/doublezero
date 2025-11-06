use anyhow::{Context, Result, ensure};
use base64::Engine;
use serde::{Deserialize, Serialize};
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

const JUPITER_LEGACY_SWAP_INSTRUCTIONS_ENDPOINT: &str =
    "https://lite-api.jup.ag/swap/v1/swap-instructions";

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum JupiterPriorityLevel {
    #[default]
    Medium,
    High,
    VeryHigh,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterPrioritizationFeeLamports {
    pub priority_level_with_max_lamports: JupiterPriorityLevelWithMaxLamports,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterPriorityLevelWithMaxLamports {
    pub max_lamports: u64,
    pub priority_level: JupiterPriorityLevel,
    pub global: bool,
}

// Jupiter's instruction format -> needs conversion to Solana SDK's Instruction
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterInstruction {
    pub program_id: String,
    pub accounts: Vec<JupiterAccountMeta>,
    /// Base64 encoded data.
    pub data: String,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterAccountMeta {
    pub pubkey: String,
    pub is_signer: bool,
    pub is_writable: bool,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterLegacySwapInstructionsResponse {
    pub compute_budget_instructions: Vec<JupiterInstruction>,
    pub setup_instructions: Vec<JupiterInstruction>,
    pub swap_instruction: JupiterInstruction,
    pub cleanup_instruction: Option<JupiterInstruction>,
    pub other_instructions: Vec<JupiterInstruction>,
    pub address_lookup_table_addresses: Vec<String>,
}

impl TryFrom<JupiterInstruction> for Instruction {
    type Error = anyhow::Error;

    fn try_from(instruction: JupiterInstruction) -> Result<Self> {
        let JupiterInstruction {
            program_id,
            accounts,
            data,
        } = instruction;

        let accounts = accounts
            .into_iter()
            .map(
                |JupiterAccountMeta {
                     pubkey,
                     is_signer,
                     is_writable,
                 }| {
                    Ok(AccountMeta {
                        pubkey: Pubkey::from_str_const(&pubkey),
                        is_signer,
                        is_writable,
                    })
                },
            )
            .collect::<Result<_>>()?;

        Ok(Instruction {
            program_id: Pubkey::from_str_const(&program_id),
            accounts,
            data: base64::engine::general_purpose::STANDARD.decode(&data)?,
        })
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterLegacySwapInstructionsRequest {
    pub user_public_key: String,
    pub quote_response: super::quote::JupiterLegacyQuoteResponse,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prioritization_fee_lamports: Option<JupiterPrioritizationFeeLamports>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dynamic_compute_unit_limit: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub wrap_and_unwrap_sol: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub as_legacy_transaction: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub skip_user_accounts_rpc_calls: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dynamic_slippage: Option<bool>,
}

impl JupiterLegacySwapInstructionsRequest {
    pub async fn try_execute(&self) -> Result<JupiterLegacySwapInstructionsResponse> {
        let client = reqwest::Client::new();

        let response = client
            .post(JUPITER_LEGACY_SWAP_INSTRUCTIONS_ENDPOINT)
            .json(self)
            .send()
            .await?;
        ensure!(
            response.status().is_success(),
            "Jupiter legacy swap instructions request failed"
        );

        response
            .json()
            .await
            .context("Malformed Jupiter legacy swap instructions response")
    }
}
