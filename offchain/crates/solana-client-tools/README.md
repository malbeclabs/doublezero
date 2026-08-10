# doublezero-solana-client-tools

Shared Solana client helpers for the offchain crates: wallet and payer handling,
keypair loading, RPC and account fetching, instruction batching, and transaction
building.

TODO: document the rest of the crate.

## Squads

Squads multisig support for any CLI whose instructions need a vault to authorize
them. Behind the `squads` feature, which is on by default.

A Squads vault is a PDA, so no local keypair can sign for it. Any instruction the
vault must authorize is therefore built against the vault, encoded, and imported
into the Squads UI, where the members approve and execute it. This module holds
the parts of that flow worth having in one place: deriving the vault, checking
that the multisig is real before anything irreversible happens, and encoding the
result in the form the UI accepts.

### Command-line options

Flatten one of these into a clap command to give it a Squads mode. Which one
depends on whether the command always acts as a vault.

| Struct               | `--multisig` | Use for                                    |
|----------------------|--------------|--------------------------------------------|
| `SquadsArgs`         | required     | A command that only ever acts as a vault.  |
| `OptionalSquadsArgs` | optional     | An existing command gaining a Squads mode. |

`OptionalSquadsArgs` is the migration shape. Naming a multisig switches the
command over to the vault. Leaving it off keeps the wallet behavior the command
already had, so adopting Squads does not mean reshaping every subcommand:

```rust
#[command(flatten)]
squads: OptionalSquadsArgs,
```

```rust
if let Some(vault_key) = squads.try_find_vault_address(&connection).await? {
    // Build against the vault and print it for import.
} else {
    // Sign with the wallet, as before.
}
```

Both structs also take `--vault-index <U8>`, defaulting to vault 0. `SquadsArgs`
additionally carries `--allow-vault-subaccounts`, which only
`try_find_handover_vault_address` reads.

**Choose your entry point by whether the vault is about to receive an authority.**

| Method                                  | Use when                                        |
|-----------------------------------------|-------------------------------------------------|
| `try_find_handover_vault_address(conn)` | About to hand an authority to the vault.        |
| `try_find_vault_address(conn)`          | Acting as a vault that already holds something. |

The handover variant additionally refuses a non-zero vault index on Solana mainnet,
where Squads offers only vault 0 unless the multisig is on a paid plan with
subaccounts. Every index derives a well-formed PDA and nothing onchain
distinguishes the two, so it refuses rather than verifies, and
`--allow-vault-subaccounts` overrides it. The network comes from the genesis hash
rather than the URL, and is read only when a non-zero index is named, so the
default path costs nothing.

The plain variant carries no such check because a caller acting as a vault that
already holds something has better proof than the check could offer. Reach for the
handover variant whenever being wrong about the vault cannot be undone.

### Deriving and verifying the vault

`try_find_vault_address` verifies the multisig before deriving, which is the
important part. Pubkeys carry no checksum, so a mistyped `--multisig` still
derives a perfectly well-formed vault PDA. Handing an authority to one of those
cannot be undone: nothing can ever sign for the vault of a multisig that does
not exist.

| Function                                    | Purpose                                                |
|---------------------------------------------|--------------------------------------------------------|
| `try_verify_multisig(connection, multisig)` | Confirms the account exists and is owned by Squads v4. |
| `find_vault_address(multisig, vault_index)` | Pure PDA derivation, no RPC.                           |
| `SQUADS_V4_PROGRAM_ID`                      | The program the vault is derived under.                |

The derivation uses the seeds `"multisig"`, the multisig account, `"vault"`, and
the vault index. It is covered by a known-answer test against a real devnet
multisig and its vault.

### Emitting a transaction for the UI

```rust
print_vault_transaction(&connection, &vault_key, &[instruction]);
```

Alongside the base58 payload this prints an explorer transaction inspector link
that decodes it, so the caller can read the instruction back rather than import an
opaque blob. The link carries the same base58 payload, so there is one encoding to
reason about. The inspector reads that parameter with `atob` and therefore falls back
to its input box, which retries base58 and renders the transaction, at the cost of
dropping the parameter from the address bar. Reloading an opened link loses it, so
open the link fresh rather than passing a reloaded tab's URL on.

The link passes the connection's own endpoint as `cluster=custom&customUrl=`,
percent-encoded, rather than naming a cluster. A Squads deployment on another SVM
network is not something the explorer's cluster list covers, and pointing it back
at the endpoint this command used works whatever the network is.

That means the endpoint travels inside the link. When it is not one of Solana's
known-harmless URLs, meaning Solana's three public endpoints and
`http://localhost:8899`, the output carries a warning not to share the link where
the endpoint should not go. The warning does not claim the endpoint is a secret, only
that it is now part of the link, since an endpoint on another SVM network is
unrecognizable from here and that says nothing either way about how sensitive it
is. Whether it matters is the caller's call.

`encode_vault_transaction` is the same thing without the surrounding output, for
callers that want the string.

This is a wire contract with the Squads UI, so it is worth stating exactly: a
base58 encoded legacy message, the vault as fee payer and sole signer, and a
zeroed placeholder blockhash that Squads replaces when it wraps the instructions
into a vault transaction. How members import it differs by UI.

The format is pinned by a test that decodes the output back and asserts those
properties, and it has been confirmed by executing emitted transactions against a
devnet Squad.

### Reading loader-v3 authorities

Deciding whether a vault already controls a program, or a buffer, means reading
the loader's own accounts. Both decodes take an already-deserialized
`UpgradeableLoaderState` so a caller that has fetched the account does not fetch
it twice.

| Function                       | Returns                                                 |
|--------------------------------|---------------------------------------------------------|
| `try_upgrade_authority(state)` | The program's upgrade authority, from its program data. |
| `try_buffer_authority(state)`  | The buffer's authority.                                 |

Both return `Result<Option<Pubkey>>`. The error means the account is not the kind
asked for, and names what it is instead. `Ok(None)` means the account is that
kind but immutable, which is a real state rather than a malformed account, so
what it means is left to the caller.
