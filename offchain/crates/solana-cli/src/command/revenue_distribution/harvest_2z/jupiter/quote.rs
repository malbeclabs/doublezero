use anyhow::Result;
use serde::{Deserialize, Serialize};

use super::JupiterClient;

const JUPITER_QUOTE_PATH: &str = "/swap/v1/quote";

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub enum JupiterSwapMode {
    #[default]
    ExactIn,
    ExactOut,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub enum JupiterInstructionVersion {
    #[default]
    V1,
    V2,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterLegacyQuoteRequest {
    /// Max value: 10_000 (100%).
    pub slippage_bps: u16,

    pub swap_mode: JupiterSwapMode,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub only_direct_routes: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub restrict_intermediate_tokens: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_accounts: Option<u8>,

    pub instruction_version: JupiterInstructionVersion,

    pub amount: u64,

    pub output_mint: String,

    pub input_mint: String,

    /// NOTE: Only supports one dex at a time.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dexes: Option<String>,
}

impl JupiterLegacyQuoteRequest {
    pub async fn try_execute(&self, client: &JupiterClient) -> Result<JupiterLegacyQuoteResponse> {
        client.get(JUPITER_QUOTE_PATH, self).await
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JupiterLegacyQuoteResponse {
    pub input_mint: String,
    pub in_amount: String,
    pub output_mint: String,
    pub out_amount: String,
    pub other_amount_threshold: String,
    pub swap_mode: JupiterSwapMode,
    pub slippage_bps: u16,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub platform_fee: Option<u16>,
    pub price_impact_pct: String,
    pub route_plan: Vec<super::JupiterRoutePlan>,
}
