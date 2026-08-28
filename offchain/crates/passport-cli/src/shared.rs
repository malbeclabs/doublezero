use clap::Args;
use solana_sdk::pubkey::Pubkey;

/// Arguments shared by the access-request verbs (`prepare-validator-access`,
/// `request-validator-access`). Field names and flags are unchanged from the
/// pre-RFC-20 CLI.
#[derive(Debug, Args, Clone)]
pub struct SharedAccessArgs {
    /// The DoubleZero service key to request access from
    #[arg(long)]
    pub doublezero_address: Pubkey,
    /// The validator's node ID (identity pubkey)
    #[arg(long, value_name = "PUBKEY")]
    pub primary_validator_id: Pubkey,
    /// Optional backup validator IDs (identity pubkeys)
    #[arg(long, value_name = "PUBKEY,PUBKEY,PUBKEY", value_delimiter = ',')]
    pub backup_validator_ids: Vec<Pubkey>,
    /// Number of previous epochs to check when evaluating the leader schedule (defaults to ENV_PREVIOUS_LEADER_EPOCHS)
    #[arg(long, hide = true)]
    pub leader_schedule_epochs: Option<u8>,
}
