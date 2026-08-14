use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use percent_encoding::{AsciiSet, NON_ALPHANUMERIC, utf8_percent_encode};
use solana_loader_v3_interface::state::UpgradeableLoaderState;
use solana_message::Message;
use solana_sdk::{instruction::Instruction, pubkey, pubkey::Pubkey};

use crate::{
    rpc::{NetworkEnvironment, SolanaConnection},
    transaction::MAX_TRANSACTION_SIZE,
};

// Squads Protocol v4 multisig program.
pub const SQUADS_V4_PROGRAM_ID: Pubkey = pubkey!("SQDS4ep65T869zMMBKyuUq6aD6EgTu8psMjkvj52pCf");

const VAULT_SEED_PREFIX: &[u8] = b"multisig";
const VAULT_SEED: &[u8] = b"vault";

// A payload travels as the `transaction_message` of `vault_transaction_create`, and the
// app bundles the compute budget pair, a `proposal_create`, and a `proposal_approve` into
// that same transaction, which is how one transaction takes a payload all the way to the
// approvers. That transaction is what has to fit MAX_TRANSACTION_SIZE, so what it spends
// is what a payload does not get to spend. Each block below adds up to the byte count its
// assertion names, and the total is the reserve.

// vault_transaction_create, as a legacy transaction.
const VAULT_TRANSACTION_CREATE_BYTES: usize = 1 // signature count (shortvec)
    + 64 // creator signature
    + 3 // message header
    + 1 // account key count (shortvec)
    + 32 // multisig key
    + 32 // transaction account key
    + 32 // member key, both creator and rent payer
    + 32 // system program key
    + 32 // Squads program key
    + 32 // recent blockhash
    + 1 // instruction count (shortvec)
    + 1 // program id index
    + 1 // account index count (shortvec)
    + 5 // account indexes
    // Instruction data length (shortvec), 2 bytes once a payload pushes the data past 127.
    + 2
    + 8 // instruction discriminator
    + 1 // vault_index
    + 1 // ephemeral_signers
    + 4 // transaction_message length (borsh Vec<u8> prefix)
    + 1; // memo: None
const _: () = assert!(VAULT_TRANSACTION_CREATE_BYTES == 286);

// The compute budget pair the app bundles.
const COMPUTE_BUDGET_PAIR_BYTES: usize = 32 // compute budget program key
    + 1 // program id index (limit)
    + 1 // account index count (shortvec)
    + 1 // instruction data length (shortvec)
    + 5 // SetComputeUnitLimit payload, a discriminator and a u32
    + 1 // program id index (price)
    + 1 // account index count (shortvec)
    + 1 // instruction data length (shortvec)
    + 9; // SetComputeUnitPrice payload, a discriminator and a u64
const _: () = assert!(COMPUTE_BUDGET_PAIR_BYTES == 52);

// The proposal_create the app bundles, whose other four accounts are already keys of the
// create instruction.
const PROPOSAL_CREATE_BYTES: usize = 32 // proposal account key
    + 1 // program id index
    + 1 // account index count (shortvec)
    + 5 // account indexes
    + 1 // instruction data length (shortvec)
    + 8 // instruction discriminator
    + 8 // transaction_index, a u64
    + 1; // draft
const _: () = assert!(PROPOSAL_CREATE_BYTES == 57);

// The proposal_approve the app bundles, all of whose accounts are already keys.
const PROPOSAL_APPROVE_BYTES: usize = 1 // program id index
    + 1 // account index count (shortvec)
    + 3 // account indexes
    + 1 // instruction data length (shortvec)
    + 8 // instruction discriminator
    + 1; // memo: None
const _: () = assert!(PROPOSAL_APPROVE_BYTES == 15);

// What Squads spends around a payload, against the length that payload measures as a
// legacy message.
const VAULT_TRANSACTION_RESERVED_BYTES: usize = VAULT_TRANSACTION_CREATE_BYTES
    + COMPUTE_BUDGET_PAIR_BYTES
    + PROPOSAL_CREATE_BYTES
    + PROPOSAL_APPROVE_BYTES
    + 2 // in case the app compiles that transaction as v0 rather than legacy
    - 32 // recent blockhash a Squads TransactionMessage does not carry
    + 1 // address_table_lookups length a Squads TransactionMessage always writes
    + 3; // rounding up the 381 of everything above
const _: () = assert!(VAULT_TRANSACTION_RESERVED_BYTES == 384);

// Three Squads app behaviors the terms above assume away are not program schema, and the
// three bytes of rounding cover none of them:
//      +4  and the text, per memo typed at import, on the create instruction and again on
//          the approval, where text of 115 bytes or more costs one further byte for the
//          approval's own data length prefix. Borsh writes Some(String) as a tag, a
//          4-byte length and the text where None writes one byte, and nothing bounds the
//          text, so no reserve can cover a memo. It spends from a payload's own headroom,
//          which is why a payload sized to the last byte is a payload that a memo breaks.
//     +96  for a rent payer separate from the creator, being 32 for the key and 64 for
//          its signature. The app pays from the connected wallet, which is also the
//          creator, so this is assumed away rather than reserved for.
//     +33  per further account key the wrapper carries, being 32 for the key and 1 for
//          the index of it, and more where that key arrives with an instruction of its
//          own. A tip account paid by a System transfer costs 49.

// Payload instructions run as separate invocations from vault_transaction_execute,
// against a runtime instruction trace that holds 64 entries for the whole transaction.
// Execute spends three of them on the compute budget pair and on itself, leaving around
// 61, and fewer for every payload instruction that invokes further.
//
// 48 is an arbitrarily conservative lower bound, not that ceiling. Nothing here measures
// how deep a payload's own invocations go, so the limit leaves room for them rather than
// pricing them, and a real payload needing more is the reason to raise it.
pub const MAX_PAYLOAD_INSTRUCTIONS: usize = 48;

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
pub fn try_print_vault_transaction(
    connection: &SolanaConnection,
    vault_key: &Pubkey,
    instructions: &[Instruction],
) -> Result<()> {
    let encoded = try_encode_vault_transaction(vault_key, instructions)?;
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

    Ok(())
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

/// Bytes `Message::serialize` may produce for a payload of `instruction_count`
/// instructions, and not the length of the base58 the encoder returns. Sizing to the last
/// byte leaves no room for a memo typed at import.
pub fn vault_transaction_payload_budget(instruction_count: usize) -> usize {
    // A Squads TransactionMessage writes each instruction's data length as a u16
    // where a legacy message writes it in one byte, so every payload instruction
    // costs one byte more once wrapped than the payload measures here. The term
    // stops being spent at 128 data bytes, where the legacy prefix grows to two as
    // well, which makes it conservative rather than exact.
    //
    // Const subtraction on purpose, so a reserve raised past the transaction limit is a
    // build failure rather than a budget of zero that refuses every payload.
    (MAX_TRANSACTION_SIZE - VAULT_TRANSACTION_RESERVED_BYTES).saturating_sub(instruction_count)
}

/// Encode instructions as the base58 payload the Squads UI imports, refusing one
/// that will not fit the transaction Squads wraps around it.
pub fn try_encode_vault_transaction(
    vault_key: &Pubkey,
    instructions: &[Instruction],
) -> Result<String> {
    // An empty payload encodes and imports perfectly well, and costs a human the
    // approvals to execute nothing.
    ensure!(
        !instructions.is_empty(),
        "no instructions to encode for the vault"
    );
    ensure!(
        instructions.len() <= MAX_PAYLOAD_INSTRUCTIONS,
        "payload has {} instructions, over the {MAX_PAYLOAD_INSTRUCTIONS} a Squads vault \
         transaction is allowed here. Each one runs as its own invocation against an \
         instruction trace the whole execute transaction shares",
        instructions.len()
    );

    // The wire contract with that UI: a legacy message, the vault as fee payer and
    // sole signer, and a placeholder blockhash Squads replaces when it wraps the
    // instructions into a vault transaction.
    let message = Message::new(instructions, Some(vault_key));

    // Squads signs for the vault. Any other signer the payload names has to sign the
    // execute transaction itself, which the UI can only arrange for the member running
    // it, and nothing here knows who that will be. Such a payload imports and collects
    // approvals before running out of signers, unlike an oversized one, which is refused
    // at import.
    ensure!(
        message.header.num_required_signatures == 1,
        "payload requires {} signatures. Squads signs for the vault, and any other signer the \
         payload names has to sign the execute transaction itself, which cannot be arranged for a \
         key chosen when the payload was written",
        message.header.num_required_signatures
    );

    let account_key_count = message.account_keys.len();
    let payload = message.serialize();

    let budget = vault_transaction_payload_budget(instructions.len());
    ensure!(
        payload.len() <= budget,
        "payload is {} bytes, {} over the {budget} a Squads vault transaction can \
         carry. Squads spends {VAULT_TRANSACTION_RESERVED_BYTES} of the \
         {MAX_TRANSACTION_SIZE}-byte transaction limit wrapping a payload into a \
         create transaction alongside its proposal and approval, plus a byte per \
         payload instruction. Emit fewer or smaller instructions",
        payload.len(),
        payload.len() - budget
    );

    // By execute the payload's instruction data lives in the transaction account and
    // does not travel, which leaves execute looser than create at every size, so this
    // cannot fire on a payload create accepted. Checked anyway, so nothing certifies a
    // payload that imports and then cannot execute.
    let execute_size = vault_transaction_execute_size(account_key_count);
    ensure!(
        execute_size <= MAX_TRANSACTION_SIZE,
        "the payload's {account_key_count} account keys need a {execute_size}-byte transaction to \
         execute, over the {MAX_TRANSACTION_SIZE}-byte limit. Emit instructions touching fewer \
         accounts"
    );

    Ok(bs58::encode(payload).into_string())
}

// vault_transaction_execute, as a legacy transaction bundled with the same compute budget
// pair the app puts alongside create.
const VAULT_TRANSACTION_EXECUTE_BYTES: usize = 1 // signature count (shortvec)
    + 64 // member signature
    + 3 // message header
    + 1 // account key count (shortvec)
    + 32 // multisig key
    + 32 // proposal key
    + 32 // transaction account key
    + 32 // member key
    + 32 // Squads program key
    + 32 // recent blockhash
    + 1 // instruction count (shortvec)
    + 1 // program id index
    + 1 // account index count (shortvec)
    + 4 // account indexes
    + 1 // instruction data length (shortvec)
    + 8 // instruction discriminator
    + COMPUTE_BUDGET_PAIR_BYTES;
const _: () = assert!(VAULT_TRANSACTION_EXECUTE_BYTES == 329);

// What each payload account key costs the transaction carrying it, whether that is the
// payload's own message or the execute that passes it as a remaining account.
const PER_ACCOUNT_KEY_BYTES: usize = 32 // the key
    + 1; // the index of it
const _: () = assert!(PER_ACCOUNT_KEY_BYTES == 33);

fn vault_transaction_execute_size(account_key_count: usize) -> usize {
    VAULT_TRANSACTION_EXECUTE_BYTES + PER_ACCOUNT_KEY_BYTES * account_key_count
}

#[cfg(test)]
mod tests {
    use solana_compute_budget_interface::ComputeBudgetInstruction;
    use solana_sdk::instruction::AccountMeta;

    use super::*;

    const MULTISIG_KEY: Pubkey = pubkey!("6GRbcdzDCYdCddcZZBU5ziKXxcKDAcTGyfFPibvSyZgP");
    const VAULT_KEY: Pubkey = pubkey!("2KAv4oDNwoB9oy6ja29jZ38KPqUqQiJpnaHr5QS4yTd3");
    const PROGRAM_KEY: Pubkey = pubkey!("a1oQyDEkMKk8PLcKTXgAVP8C9g4HUBAmB564Dvszh6F");

    // What a single-instruction payload spends around that instruction's data and the
    // data's own length prefix.
    const AROUND_INSTRUCTION_DATA: usize = 3 // message header
        + 1 // account key count (shortvec)
        + 32 // vault key, the fee payer
        + 32 // multisig key, the instruction's one account
        + 32 // program key
        + 32 // recent blockhash
        + 1 // instruction count (shortvec)
        + 1 // program id index
        + 1 // account index count (shortvec)
        + 1; // account index
    const _: () = assert!(AROUND_INSTRUCTION_DATA == 136);

    // The same framing for a payload of unique accounts and no instruction data, which
    // spends an instruction data length where the one above spends an account index.
    const AROUND_ACCOUNT_KEYS: usize = 3 // message header
        + 1 // account key count (shortvec)
        + 32 // vault key, the fee payer
        + 32 // program key
        + 32 // recent blockhash
        + 1 // instruction count (shortvec)
        + 1 // program id index
        + 1 // account index count (shortvec)
        + 1; // instruction data length (shortvec)
    const _: () = assert!(AROUND_ACCOUNT_KEYS == 104);

    // A second instruction over the same account and program, before its own data and
    // that data's length prefix.
    const AROUND_SECOND_INSTRUCTION: usize = 1 // program id index
        + 1 // account index count (shortvec)
        + 1; // account index
    const _: () = assert!(AROUND_SECOND_INSTRUCTION == 3);

    fn instruction_with_data_len(data_len: usize) -> Instruction {
        let data = vec![0; data_len];

        Instruction::new_with_bytes(
            PROGRAM_KEY,
            &data,
            vec![AccountMeta::new_readonly(MULTISIG_KEY, false)],
        )
    }

    fn instruction_with_account_count(account_count: usize) -> Instruction {
        let accounts = (0..account_count)
            .map(|_| AccountMeta::new_readonly(Pubkey::new_unique(), false))
            .collect();

        Instruction::new_with_bytes(PROGRAM_KEY, &[], accounts)
    }

    fn payload_len(instructions: &[Instruction]) -> usize {
        let encoded = try_encode_vault_transaction(&VAULT_KEY, instructions).unwrap();

        bs58::decode(encoded).into_vec().unwrap().len()
    }

    #[test]
    fn test_inspector_url_carries_the_message_squads_takes() {
        let encoded =
            try_encode_vault_transaction(&VAULT_KEY, &[instruction_with_data_len(3)]).unwrap();
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
    fn test_encoded_payload_makes_the_vault_the_sole_signer() {
        let encoded =
            try_encode_vault_transaction(&VAULT_KEY, &[instruction_with_data_len(3)]).unwrap();
        let message: Message =
            bincode::deserialize(&bs58::decode(&encoded).into_vec().unwrap()).unwrap();

        // The zeroed blockhash is correct: Squads overwrites it when wrapping
        // this into a vault transaction.
        assert_eq!(message.account_keys[0], VAULT_KEY);
        assert_eq!(message.header.num_required_signatures, 1);
        assert_eq!(message.recent_blockhash, Default::default());
        assert_eq!(message.instructions.len(), 1);
    }

    #[test]
    fn test_budget_spends_one_byte_per_payload_instruction() {
        // 1_232 - 384, then one byte per instruction for the u16 data length a Squads
        // TransactionMessage writes where a legacy message writes one byte.
        assert_eq!(vault_transaction_payload_budget(0), 848);
        assert_eq!(vault_transaction_payload_budget(1), 847);
        assert_eq!(vault_transaction_payload_budget(4), 844);
    }

    #[test]
    fn test_payload_length_prefix_grows_where_the_per_instruction_term_stops() {
        assert_eq!(
            payload_len(&[instruction_with_data_len(3)]),
            AROUND_INSTRUCTION_DATA + 1 + 3
        );

        // The legacy prefix grows to two bytes here, which is where the budget's
        // per-instruction term stops buying anything.
        assert_eq!(
            payload_len(&[instruction_with_data_len(127)]),
            AROUND_INSTRUCTION_DATA + 1 + 127
        );
        assert_eq!(
            payload_len(&[instruction_with_data_len(128)]),
            AROUND_INSTRUCTION_DATA + 2 + 128
        );
    }

    #[test]
    fn test_encoder_takes_the_budget_the_accessor_reports_and_refuses_one_byte_more() {
        let budget = vault_transaction_payload_budget(1);

        // The framing above, plus the 2-byte legacy length prefix that much data needs.
        let data_len = budget - (AROUND_INSTRUCTION_DATA + 2);

        assert_eq!(payload_len(&[instruction_with_data_len(data_len)]), budget);

        let error =
            try_encode_vault_transaction(&VAULT_KEY, &[instruction_with_data_len(data_len + 1)])
                .unwrap_err()
                .to_string();

        // The numbers rather than the explanation around them, so rewording the message
        // is not a test change.
        assert!(
            error.starts_with(&format!(
                "payload is {} bytes, 1 over the {budget} ",
                budget + 1
            )),
            "{error}"
        );
    }

    #[test]
    fn test_encoder_spends_the_per_instruction_byte_on_a_second_instruction() {
        let budget = vault_transaction_payload_budget(2);

        let data_len = budget
            - (AROUND_INSTRUCTION_DATA
                + 1 // the first instruction's data length prefix
                + 3 // the first instruction's data
                + AROUND_SECOND_INSTRUCTION
                + 2); // the second instruction's data length prefix
        let instructions = [
            instruction_with_data_len(3),
            instruction_with_data_len(data_len),
        ];

        assert_eq!(payload_len(&instructions), budget);

        let over = [
            instruction_with_data_len(3),
            instruction_with_data_len(data_len + 1),
        ];
        assert!(try_encode_vault_transaction(&VAULT_KEY, &over).is_err());
    }

    #[test]
    fn test_create_binds_before_execute_at_the_widest_payload_the_budget_takes() {
        // Account keys are all execute charges for, so the payload that gets closest to
        // its limit is unique accounts and no instruction data.
        let account_count =
            (vault_transaction_payload_budget(1) - AROUND_ACCOUNT_KEYS) / PER_ACCOUNT_KEY_BYTES;

        assert_eq!(
            payload_len(&[instruction_with_account_count(account_count)]),
            AROUND_ACCOUNT_KEYS + PER_ACCOUNT_KEY_BYTES * account_count
        );
        assert!(
            try_encode_vault_transaction(
                &VAULT_KEY,
                &[instruction_with_account_count(account_count + 1)]
            )
            .is_err()
        );

        // The vault and the program are account keys of that payload too, and execute
        // passes every one of them as a remaining account. Failing here means execute
        // has become the binding constraint and the check in the encoder can now fire.
        assert!(vault_transaction_execute_size(account_count + 2) <= MAX_TRANSACTION_SIZE);
    }

    #[test]
    fn test_cannot_encode_when_there_are_no_instructions() {
        let error = try_encode_vault_transaction(&VAULT_KEY, &[]).unwrap_err();

        // Named rather than any error, since an empty payload is inside every size
        // check and would otherwise pass this test for the wrong reason.
        assert_eq!(error.to_string(), "no instructions to encode for the vault");
    }

    #[test]
    fn test_cannot_encode_more_instructions_than_execute_runs() {
        let within = vec![instruction_with_data_len(0); MAX_PAYLOAD_INSTRUCTIONS];
        let over = vec![instruction_with_data_len(0); MAX_PAYLOAD_INSTRUCTIONS + 1];

        // Both are far inside the byte budget, so the count is the only thing refusing
        // the second.
        assert!(payload_len(&within) < vault_transaction_payload_budget(within.len()));

        let error = try_encode_vault_transaction(&VAULT_KEY, &over).unwrap_err();
        assert!(
            error
                .to_string()
                .starts_with(&format!("payload has {} instructions", over.len())),
            "{error}"
        );
    }

    #[test]
    fn test_cannot_encode_when_an_instruction_names_another_signer() {
        let instruction = Instruction::new_with_bytes(
            PROGRAM_KEY,
            &[],
            vec![AccountMeta::new(Pubkey::new_unique(), true)],
        );
        let error = try_encode_vault_transaction(&VAULT_KEY, &[instruction]).unwrap_err();

        assert!(
            error
                .to_string()
                .starts_with("payload requires 2 signatures"),
            "{error}"
        );
    }

    #[test]
    fn test_the_wrapper_around_a_budget_sized_payload_still_fits() {
        let budget = vault_transaction_payload_budget(1);
        let payload_size = payload_len(&[instruction_with_data_len(
            budget - (AROUND_INSTRUCTION_DATA + 2),
        )]);
        assert_eq!(payload_size, budget);

        // What Squads stores in place of the payload's own message. The last term is what
        // the budget charges rather than what this payload spends, whose data is past 127
        // and so already carries a two-byte legacy prefix.
        let transaction_message_len = payload_size
            - 32 // recent blockhash a TransactionMessage does not carry
            + 1 // address_table_lookups length it always writes
            + 1; // the u16 data length, per payload instruction

        // The transaction the app submits at import. Only keys and data lengths decide
        // its size, so the discriminators and arguments are stand-ins of the right size.
        let member_key = Pubkey::new_unique();
        let transaction_key = Pubkey::new_unique();
        let proposal_key = Pubkey::new_unique();
        let system_program_key = Pubkey::new_unique();

        let create_data_len = 8 // instruction discriminator
            + 1 // vault_index
            + 1 // ephemeral_signers
            + 4 // transaction_message length (borsh Vec<u8> prefix)
            + transaction_message_len
            + 1; // memo: None
        let proposal_create_data_len = 8 // instruction discriminator
            + 8 // transaction_index, a u64
            + 1; // draft
        let proposal_approve_data_len = 8 // instruction discriminator
            + 1; // memo: None

        let wrapper = Message::new(
            &[
                ComputeBudgetInstruction::set_compute_unit_limit(0),
                ComputeBudgetInstruction::set_compute_unit_price(0),
                Instruction::new_with_bytes(
                    SQUADS_V4_PROGRAM_ID,
                    &vec![0; create_data_len],
                    vec![
                        AccountMeta::new(MULTISIG_KEY, false),
                        AccountMeta::new(transaction_key, false),
                        AccountMeta::new_readonly(member_key, true),
                        AccountMeta::new(member_key, true),
                        AccountMeta::new_readonly(system_program_key, false),
                    ],
                ),
                Instruction::new_with_bytes(
                    SQUADS_V4_PROGRAM_ID,
                    &vec![0; proposal_create_data_len],
                    vec![
                        AccountMeta::new_readonly(MULTISIG_KEY, false),
                        AccountMeta::new(proposal_key, false),
                        AccountMeta::new_readonly(member_key, true),
                        AccountMeta::new(member_key, true),
                        AccountMeta::new_readonly(system_program_key, false),
                    ],
                ),
                Instruction::new_with_bytes(
                    SQUADS_V4_PROGRAM_ID,
                    &vec![0; proposal_approve_data_len],
                    vec![
                        AccountMeta::new_readonly(MULTISIG_KEY, false),
                        AccountMeta::new(member_key, true),
                        AccountMeta::new(proposal_key, false),
                    ],
                ),
            ],
            Some(&member_key),
        );

        let wrapper_size = 1 // signature count (shortvec)
            + 64 // member signature
            + wrapper.serialize().len()
            + 2; // what a v0 compile would add over legacy

        // The reserve rounds 381 up to 384, so a payload sized to the budget leaves
        // exactly three bytes unspent. Failing here means the derivation beside the
        // constant no longer describes the transaction Squads builds.
        assert_eq!(wrapper_size, MAX_TRANSACTION_SIZE - 3);
    }
}
