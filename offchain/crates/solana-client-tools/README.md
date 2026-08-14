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
try_print_vault_transaction(&connection, &vault_key, &[instruction])?;
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

`try_encode_vault_transaction` is the same thing without the surrounding output, for
callers that want the string.

This is a wire contract with the Squads UI, so it is worth stating exactly: a
base58 encoded legacy message, the vault as fee payer and sole signer, and a
zeroed placeholder blockhash that Squads replaces when it wraps the instructions
into a vault transaction. How members import it differs by UI.

The format is pinned by a test that decodes the output back and asserts those
properties, and it has been confirmed by executing emitted transactions against a
devnet Squad.

### Sizing a payload

The transaction that has to carry a payload is the one Squads wraps around it, not
the payload itself. That wrapper is a `vault_transaction_create` carrying the payload
as its `transaction_message`, bundled with the compute budget pair, a
`proposal_create`, and a `proposal_approve`, which is how one transaction takes a
payload all the way to the approvers. A payload that overruns it is refused at import,
before any approval exists.

| Function                                              | Purpose                                  |
|-------------------------------------------------------|------------------------------------------|
| `vault_transaction_payload_budget(instruction_count)` | Bytes the payload's message may occupy.  |
| `try_encode_vault_transaction(vault, instructions)`   | Encodes, or refuses an unusable payload. |

The budget governs the serialized legacy message, meaning
`Message::serialize().len()`, not the base58 string the encoder returns, which is
around 1.37 times longer. A caller sizing its own instruction measures the former.

It is `MAX_TRANSACTION_SIZE` less a 384-byte reserve, less one byte per payload
instruction. The derivation, and the Squads app behavior it deliberately does not
cover, sit beside the constant. A memo typed at import is unbounded, so no reserve can
cover one, which is the reason a payload sized to the last byte is a payload a memo
breaks. A test assembles the wrapper and asserts that a payload sized to the budget
still fits, so the arithmetic beside the constant is executable rather than asserted.

Size is not the only thing that makes a payload unusable, and the other two checks
matter more, because a size failure surfaces at import while these surface at execute,
after the approvals are spent. The encoder refuses a payload naming a signer other than
the vault, since any such key has to sign the execute transaction itself and nothing
here knows who will run it. It refuses more than 48 instructions, since each runs as
its own invocation against an instruction trace the whole execute transaction shares.
That 48 is an arbitrarily conservative lower bound, not the runtime's limit.

An empty payload is refused too, which is a courtesy rather than a correctness matter:
it imports and executes nothing, at the cost of the approvals.

`vault_transaction_execute` is checked as well, though nothing the budget accepts can
overrun it. By execute the payload's instruction data lives in the transaction account
and no longer travels, which leaves execute looser than create at every size, so create
binds. A test pins that pairing rather than leaving it a claim.

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
