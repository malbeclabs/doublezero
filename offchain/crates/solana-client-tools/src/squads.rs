use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use percent_encoding::{AsciiSet, NON_ALPHANUMERIC, utf8_percent_encode};
use solana_loader_v3_interface::state::UpgradeableLoaderState;
use solana_message::Message;
use solana_sdk::{instruction::Instruction, pubkey, pubkey::Pubkey};

use crate::rpc::{NetworkEnvironment, SolanaConnection};

// Squads Protocol v4 multisig program.
pub const SQUADS_V4_PROGRAM_ID: Pubkey = pubkey!("SQDS4ep65T869zMMBKyuUq6aD6EgTu8psMjkvj52pCf");

const VAULT_SEED_PREFIX: &[u8] = b"multisig";
const VAULT_SEED: &[u8] = b"vault";

// Squads options for a command that always acts as a vault.
#[derive(Debug, Args)]
pub struct SquadsArgs {
    /// Squads multisig account, never the vault itself.
    #[arg(long, value_name = "PUBKEY")]
    pub multisig: Pubkey,

    /// Squads vault index. Vault 0 is the default vault.
    #[arg(long, default_value_t = 0, value_name = "U8")]
    pub vault_index: u8,

    /// Assert the multisig has the Squads subaccounts a non-zero vault index
    /// needs on Solana mainnet.
    #[arg(long)]
    pub allow_vault_subaccounts: bool,
}

impl SquadsArgs {
    /// The vault to act as.
    pub async fn try_find_vault_address(&self, connection: &SolanaConnection) -> Result<Pubkey> {
        // Verify before deriving, so a mistyped key fails here rather than handing an
        // authority to a vault nothing can sign for.
        try_verify_multisig(connection, &self.multisig).await?;

        Ok(find_vault_address(&self.multisig, self.vault_index).0)
    }

    /// The vault to hand an authority to.
    pub async fn try_find_handover_vault_address(
        &self,
        connection: &SolanaConnection,
    ) -> Result<Pubkey> {
        // Named apart from `try_find_vault_address` so the irreversible path is the one
        // carrying the vault index check, rather than a check a caller has to know to
        // ask for. A command acting as a vault that already holds something has better
        // proof than that check offers, and wants the plain one.
        let vault_key = self.try_find_vault_address(connection).await?;
        self.try_refuse_unusable_vault_index(connection).await?;

        Ok(vault_key)
    }

    /// Refuse a vault index the multisig may have no way to operate.
    async fn try_refuse_unusable_vault_index(&self, connection: &SolanaConnection) -> Result<()> {
        // The rule below permits vault 0 anyway. Returning here is what keeps the
        // default path from paying for a genesis hash read to be told so.
        if self.vault_index == 0 || self.allow_vault_subaccounts {
            return Ok(());
        }

        let network = connection.try_network_environment().await.context(
            "cannot tell which network this endpoint serves, and a non-zero vault index is only \
             safe once that is known. Retry, or name an endpoint that answers getGenesisHash",
        )?;

        try_refuse_unusable_vault_index(network, self.vault_index)
    }
}

/// The rule itself, split out so every network can be covered without a request.
fn try_refuse_unusable_vault_index(network: NetworkEnvironment, vault_index: u8) -> Result<()> {
    // Nothing onchain distinguishes a multisig with subaccounts from one without, so
    // this refuses rather than verifies.
    ensure!(
        vault_index == 0 || !network.is_mainnet_beta(),
        "Squads offers only vault 0 on Solana mainnet unless the multisig is on a paid plan with \
         subaccounts, and acting as vault {vault_index} of a multisig that cannot operate it \
         strands whatever is handed to it. Drop --vault-index if vault 0 was meant, or pass \
         --allow-vault-subaccounts if this multisig really does have subaccounts"
    );

    Ok(())
}

// Squads options for a CLI whose commands normally sign with the wallet. Naming a
// multisig switches a command over to the vault: it acts in place of the wallet and
// the instruction is printed for import into Squads rather than sent, because only
// the multisig can sign for a vault.
#[derive(Debug, Args)]
pub struct OptionalSquadsArgs {
    /// Squads multisig account, never the vault itself. When set, the vault
    /// acts in place of the wallet and the instruction is printed for import
    /// into Squads instead of being sent.
    #[arg(long, value_name = "PUBKEY")]
    pub multisig: Option<Pubkey>,

    /// Squads vault index. Vault 0 is the default vault.
    #[arg(long, default_value_t = 0, value_name = "U8", requires = "multisig")]
    pub vault_index: u8,
}

impl OptionalSquadsArgs {
    /// The vault to act as, or `None` when the wallet should sign for itself.
    pub async fn try_find_vault_address(
        &self,
        connection: &SolanaConnection,
    ) -> Result<Option<Pubkey>> {
        let Some(multisig_key) = self.multisig else {
            return Ok(None);
        };

        // Verify before deriving, so a mistyped key fails here rather than producing a
        // proposal nothing can execute.
        try_verify_multisig(connection, &multisig_key).await?;

        Ok(Some(find_vault_address(&multisig_key, self.vault_index).0))
    }
}

pub fn find_vault_address(multisig_key: &Pubkey, vault_index: u8) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            VAULT_SEED_PREFIX,
            multisig_key.as_ref(),
            VAULT_SEED,
            &[vault_index],
        ],
        &SQUADS_V4_PROGRAM_ID,
    )
}

/// Confirm the account exists and is owned by Squads v4.
pub async fn try_verify_multisig(
    connection: &SolanaConnection,
    multisig_key: &Pubkey,
) -> Result<()> {
    // Pubkeys carry no checksum, so a mistyped multisig still derives a perfectly
    // well-formed vault PDA. Handing an authority to one of those is irreversible,
    // because nothing can ever sign for the vault of a multisig that does not exist.
    let multisig_account = connection
        .get_account_with_commitment(multisig_key, connection.commitment())
        .await
        .context("failed to fetch multisig")?
        .value
        .with_context(|| format!("multisig {multisig_key} does not exist on this cluster"))?;
    ensure!(
        multisig_account.owner == SQUADS_V4_PROGRAM_ID,
        "account {multisig_key} is owned by {}, not the Squads v4 program \
         {SQUADS_V4_PROGRAM_ID}. Pass the multisig account, not the vault",
        multisig_account.owner
    );

    Ok(())
}

/// The buffer's authority, or `None` when the buffer is immutable.
pub fn try_buffer_authority(state: &UpgradeableLoaderState) -> Result<Option<Pubkey>> {
    // Matched exhaustively on purpose: a variant added upstream should be a compile
    // error here, not a misleading message.
    match state {
        UpgradeableLoaderState::Buffer { authority_address } => Ok(*authority_address),
        UpgradeableLoaderState::Program { .. } => bail!("the account is an upgradeable program"),
        UpgradeableLoaderState::ProgramData { .. } => {
            bail!("the account is a program data account")
        }
        UpgradeableLoaderState::Uninitialized => bail!("the account is uninitialized"),
    }
}

/// The program's upgrade authority, or `None` when the program is immutable.
pub fn try_upgrade_authority(state: &UpgradeableLoaderState) -> Result<Option<Pubkey>> {
    // Takes the program's *program data* account state, which is where loader-v3
    // records the authority.
    match state {
        UpgradeableLoaderState::ProgramData {
            upgrade_authority_address,
            ..
        } => Ok(*upgrade_authority_address),
        UpgradeableLoaderState::Buffer { .. } => bail!("the account is a buffer"),
        UpgradeableLoaderState::Program { .. } => {
            bail!("the account is a program, not its program data")
        }
        UpgradeableLoaderState::Uninitialized => bail!("the account is uninitialized"),
    }
}

/// Print a base58 encoded transaction for import into the Squads UI.
pub fn print_vault_transaction(
    connection: &SolanaConnection,
    vault_key: &Pubkey,
    instructions: &[Instruction],
) {
    let encoded = encode_vault_transaction(vault_key, instructions);
    let rpc_url = connection.url();

    println!("Import this base58 encoded transaction into Squads:");
    println!("{encoded}");
    println!();
    println!("Read it back first:");
    println!("{}", inspector_url(&encoded, &rpc_url));

    if !is_public_solana_endpoint(&rpc_url) {
        println!();
        println!("WARNING: that link carries the endpoint this command was given.");
        println!("Do not share it anywhere that endpoint should not go.");
    }
}

/// Explorer link that decodes the message, so it can be read before it is signed.
fn inspector_url(encoded: &str, rpc_url: &str) -> String {
    // Unreserved characters per RFC 3986. The base58 alphabet is already within
    // that set, so only the endpoint needs escaping.
    const QUERY_VALUE: &AsciiSet = &NON_ALPHANUMERIC
        .remove(b'-')
        .remove(b'.')
        .remove(b'_')
        .remove(b'~');

    format!(
        "https://explorer.solana.com/tx/inspector?message={encoded}&cluster=custom&customUrl={}",
        utf8_percent_encode(rpc_url, QUERY_VALUE)
    )
}

/// Whether the endpoint is one there is nothing to be careful about sharing.
fn is_public_solana_endpoint(rpc_url: &str) -> bool {
    [
        NetworkEnvironment::PUBLIC_SOLANA_MAINNET_BETA_URL,
        NetworkEnvironment::PUBLIC_SOLANA_TESTNET_URL,
        NetworkEnvironment::PUBLIC_SOLANA_DEVNET_URL,
        // Not public, but it names nothing that exists off this machine.
        NetworkEnvironment::DEFAULT_LOCALNET_URL,
    ]
    .contains(&rpc_url.trim_end_matches('/'))
}

/// Encode instructions as the base58 payload the Squads UI imports.
pub fn encode_vault_transaction(vault_key: &Pubkey, instructions: &[Instruction]) -> String {
    // The wire contract with that UI: a legacy message, the vault as fee payer and
    // sole signer, and a placeholder blockhash Squads replaces when it wraps the
    // instructions into a vault transaction.
    bs58::encode(Message::new(instructions, Some(vault_key)).serialize()).into_string()
}

#[cfg(test)]
mod tests {
    use solana_sdk::instruction::AccountMeta;

    use super::*;

    const MULTISIG_KEY: Pubkey = pubkey!("6GRbcdzDCYdCddcZZBU5ziKXxcKDAcTGyfFPibvSyZgP");
    const VAULT_KEY: Pubkey = pubkey!("2KAv4oDNwoB9oy6ja29jZ38KPqUqQiJpnaHr5QS4yTd3");
    const PROGRAM_KEY: Pubkey = pubkey!("a1oQyDEkMKk8PLcKTXgAVP8C9g4HUBAmB564Dvszh6F");

    fn instruction() -> Instruction {
        Instruction::new_with_bytes(
            PROGRAM_KEY,
            &[1, 2, 3],
            vec![AccountMeta::new_readonly(MULTISIG_KEY, false)],
        )
    }

    #[test]
    fn test_inspector_url_carries_the_message_squads_takes() {
        let encoded = encode_vault_transaction(&VAULT_KEY, &[instruction()]);
        let url = inspector_url(&encoded, "https://api.devnet.solana.com");

        // Unescaped on purpose: the base58 alphabet is URL safe.
        assert_eq!(
            url,
            format!(
                "https://explorer.solana.com/tx/inspector?message={encoded}&cluster=custom\
                 &customUrl=https%3A%2F%2Fapi.devnet.solana.com"
            )
        );
    }

    #[test]
    fn test_inspector_url_escapes_an_endpoint_carrying_its_own_query() {
        let url = inspector_url("abc", "https://rpc.example.com/v1?api-key=secret&x=1");

        // Unescaped, the endpoint's own separators would read as parameters of
        // the explorer link rather than as part of customUrl.
        assert!(
            url.ends_with(
                "&customUrl=https%3A%2F%2Frpc.example.com%2Fv1%3Fapi-key%3Dsecret%26x%3D1"
            )
        );
    }

    #[test]
    fn test_only_known_harmless_endpoints_skip_the_sharing_warning() {
        assert!(is_public_solana_endpoint("https://api.devnet.solana.com"));
        assert!(is_public_solana_endpoint(
            "https://api.mainnet-beta.solana.com/"
        ));
        assert!(is_public_solana_endpoint(
            NetworkEnvironment::DEFAULT_LOCALNET_URL
        ));
        assert!(!is_public_solana_endpoint(
            "https://rpc.example.com/?api-key=secret"
        ));
        assert!(!is_public_solana_endpoint(
            NetworkEnvironment::PUBLIC_DOUBLEZERO_LEDGER_TESTNET_URL
        ));
    }

    #[test]
    fn test_only_solana_mainnet_refuses_a_nonzero_vault_index() {
        assert!(try_refuse_unusable_vault_index(NetworkEnvironment::MainnetBeta, 3).is_err());

        // Everywhere else the other indexes are free to use. Localnet is also
        // where any genesis hash the mapping does not recognize lands.
        for network in [
            NetworkEnvironment::Devnet,
            NetworkEnvironment::Testnet,
            NetworkEnvironment::Localnet,
        ] {
            assert!(
                try_refuse_unusable_vault_index(network, 3).is_ok(),
                "{network:?} should permit a non-zero vault index"
            );
        }
    }

    #[test]
    fn test_vault_0_is_never_refused() {
        assert!(try_refuse_unusable_vault_index(NetworkEnvironment::MainnetBeta, 0).is_ok());
    }

    #[test]
    fn test_find_vault_address_for_default_vault_index() {
        assert_eq!(find_vault_address(&MULTISIG_KEY, 0).0, VAULT_KEY);
    }

    #[test]
    fn test_authority_decodes_separate_immutable_from_the_wrong_account_kind() {
        let authority = Pubkey::new_unique();

        let buffer = UpgradeableLoaderState::Buffer {
            authority_address: Some(authority),
        };
        let immutable_buffer = UpgradeableLoaderState::Buffer {
            authority_address: None,
        };
        let program_data = UpgradeableLoaderState::ProgramData {
            slot: 0,
            upgrade_authority_address: Some(authority),
        };
        let immutable_program_data = UpgradeableLoaderState::ProgramData {
            slot: 0,
            upgrade_authority_address: None,
        };
        let program = UpgradeableLoaderState::Program {
            programdata_address: Pubkey::new_unique(),
        };

        // Present, absent, and wrong-kind, for each decode.
        assert_eq!(try_buffer_authority(&buffer).unwrap(), Some(authority));
        assert_eq!(try_buffer_authority(&immutable_buffer).unwrap(), None);
        assert!(try_buffer_authority(&program_data).is_err());
        assert!(try_buffer_authority(&program).is_err());
        assert!(try_buffer_authority(&UpgradeableLoaderState::Uninitialized).is_err());

        assert_eq!(
            try_upgrade_authority(&program_data).unwrap(),
            Some(authority)
        );
        assert_eq!(
            try_upgrade_authority(&immutable_program_data).unwrap(),
            None
        );
        assert!(try_upgrade_authority(&buffer).is_err());
        assert!(try_upgrade_authority(&program).is_err());
        assert!(try_upgrade_authority(&UpgradeableLoaderState::Uninitialized).is_err());
    }

    #[test]
    fn test_encode_vault_transaction_makes_the_vault_the_sole_signer() {
        let encoded = encode_vault_transaction(&VAULT_KEY, &[instruction()]);
        let message: Message =
            bincode::deserialize(&bs58::decode(&encoded).into_vec().unwrap()).unwrap();

        // The zeroed blockhash is correct: Squads overwrites it when wrapping
        // this into a vault transaction.
        assert_eq!(message.account_keys[0], VAULT_KEY);
        assert_eq!(message.header.num_required_signatures, 1);
        assert_eq!(message.recent_blockhash, Default::default());
        assert_eq!(message.instructions.len(), 1);
    }
}
