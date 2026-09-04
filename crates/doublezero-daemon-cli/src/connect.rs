//! `doublezero connect` — provision the operator's DoubleZero user(s).
//!
//! Orchestrates device selection (latency utilities), onchain user
//! creation/activation polling, tunnel provisioning via the daemon, and
//! multicast role assignment. Progress animation is rendered on a stderr
//! spinner (transient UI); informational and result lines route through the
//! shared writer.

use std::{collections::HashMap, io::Write, net::Ipv4Addr, str::FromStr, time::Duration};

use backon::{BlockingRetryable, ExponentialBuilder};
use clap::{Args, Subcommand, ValueEnum};
use doublezero_cli_core::CliContext;
use doublezero_ip_proof::IpOwnershipProof;
use doublezero_sdk::{
    commands::{
        multicastgroup::{
            subscribe::{UpdateMulticastGroupRolesCommand, MAX_GROUPS_PER_TRANSACTION},
            subscribe_feed::SubscribeFeedCommand,
            unsubscribe_feed::UnsubscribeFeedCommand,
        },
        user::{create::CreateUserCommand, create_subscribe::CreateSubscribeUserCommand},
    },
    Device, Exchange, Feed, User, UserCYOA, UserStatus, UserType,
};
use doublezero_serviceability::{
    processors::multicastgroup::subscribe_feed::MAX_USER_FEEDS,
    state::accesspass::{AccessPass, AccessPassType, FeedSeat},
};
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    client::{DaemonClient, LatencyRecord, StatusResponse},
    enable::ensure_reconciler_enabled,
    helpers::{init_spinner, resolve_client_ip},
    ip_proof::{HttpIpProofClient, IpProofClient, IpProofError},
    latency::{best_latency, retrieve_latencies, select_tunnel_endpoint, NoAvailableEndpoint},
    ledger::LedgerClient,
    requirements::check_daemon,
};

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum MulticastMode {
    Publisher,
    Subscriber,
}

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, Subcommand)]
pub enum DzMode {
    /// Provision a user in IBRL mode
    IBRL {
        /// provide tenant code or pubkey
        tenant: Option<String>,
        /// Allocate a new address for the user
        #[arg(short, long, default_value_t = false)]
        allocate_addr: bool,
    },
    //EdgeFiltering,
    /// Provision a user in Multicast mode
    Multicast {
        /// (Legacy) Multicast mode: Publisher or Subscriber
        #[arg(value_enum)]
        mode: Option<MulticastMode>,

        /// (Legacy) Multicast group code(s)
        #[arg(num_args = 0..)]
        multicast_groups: Vec<String>,

        /// Multicast groups to publish to
        #[arg(long = "publish", num_args = 1..)]
        pub_groups: Vec<String>,

        /// Multicast groups to subscribe to
        #[arg(long = "subscribe", num_args = 1..)]
        sub_groups: Vec<String>,

        /// Feeds to join on an EdgeSeat access pass (codes or pubkeys)
        #[arg(
            long = "subscribe-feed",
            num_args = 1..,
            conflicts_with_all = ["mode", "multicast_groups", "pub_groups", "sub_groups"]
        )]
        sub_feeds: Vec<String>,

        /// Feeds to leave on an EdgeSeat access pass (codes or pubkeys)
        #[arg(
            long = "unsubscribe-feed",
            num_args = 1..,
            conflicts_with_all = ["mode", "multicast_groups", "pub_groups", "sub_groups"]
        )]
        unsub_feeds: Vec<String>,
    },
}

/// Connect your server to a doublezero device
#[derive(Args, Debug, Default)]
pub struct Connect {
    /// Omit to provision every mode the AccessPass authorizes.
    #[clap(subcommand)]
    pub dz_mode: Option<DzMode>,

    /// Tenant code or pubkey for the IBRL user. Bare `doublezero connect` only —
    /// `doublezero connect ibrl` takes the tenant positionally.
    #[arg(long)]
    pub tenant: Option<String>,

    /// Allocate a new address for the IBRL user. Bare `doublezero connect` only —
    /// `doublezero connect ibrl` has its own `-a`.
    #[arg(long, default_value_t = false)]
    pub allocate_addr: bool,

    /// [deprecated] Client IP address — ignored; set --client-ip on the daemon (doublezerod) instead
    #[arg(long, global = true)]
    pub client_ip: Option<String>,

    /// Device Pubkey or code to associate with the user
    #[arg(long, global = true)]
    pub device: Option<String>,

    /// Base URL of the RFC-27 IP ownership verification service, overriding the environment
    /// default. Point this at a local or devnet verifier; `DZ_IP_VERIFIER_URL` does the same.
    #[arg(long, global = true)]
    pub ip_verifier_url: Option<String>,

    /// Verbose output
    #[arg(short, long, global = true, default_value_t = false)]
    pub verbose: bool,
}

/// The result of one leg of a bare `doublezero connect`.
///
/// `NoOp` is attempted-but-nothing-to-do (only `execute_multicast`'s empty-allowlist
/// return reaches it); `Skipped` was never attempted and always carries the reason,
/// because a leg that silently does nothing is indistinguishable from one that worked.
enum LegOutcome {
    Provisioned,
    NoOp,
    Skipped(String),
    Failed(eyre::Report),
}

enum ParsedDzMode {
    Ibrl(UserType, Option<String>),
    Multicast {
        pub_groups: Vec<String>,
        sub_groups: Vec<String>,
    },
    MulticastFeeds {
        sub_feeds: Vec<String>,
        unsub_feeds: Vec<String>,
    },
}

/// The join half of a feed command after validation: everything needed to execute it without
/// further checks.
struct FeedJoin {
    user: FeedJoinUser,
    feed_pks: Vec<Pubkey>,
}

enum FeedJoinUser {
    Existing(Pubkey),
    Create {
        device_pk: Pubkey,
        tunnel_endpoint: Ipv4Addr,
    },
}

/// AccessPass pre-flight: `Ok(false)` when no pass exists for `(client_ip, payer)` so the caller
/// can render its own diagnostic before bailing. With `enforce_epoch`, the pass must also cover
/// the current epoch.
///
/// [`LedgerClient::get_accesspass`] resolves through `GetAccessPassCommand`, which probes the
/// dynamic (0.0.0.0) PDA before the exact-IP one, so a wildcard-seat holder — including the
/// `EdgeSeat` passes the Feed Oracle issues — is found here without a second lookup. That is the
/// same ordering `CreateUserCommand` uses to pick the PDA it sends, and the program accepts either
/// (`smartcontract/programs/doublezero-serviceability/src/processors/user/create_core.rs`), so
/// this pre-flight accepts exactly what the transaction will.
fn check_accesspass<L: LedgerClient>(
    ledger: &L,
    client_ip: Ipv4Addr,
    enforce_epoch: bool,
) -> eyre::Result<bool> {
    let Some(accesspass) = ledger.get_accesspass(client_ip, ledger.get_payer())? else {
        return Ok(false);
    };

    if !enforce_epoch {
        return Ok(true);
    }
    let epoch = ledger.get_epoch()?;
    Ok(accesspass.last_access_epoch >= epoch)
}

/// Fetch the caller's AccessPass, rendering the same diagnostic and error that
/// [`Connect::execute`]'s single-mode path emits when none exists. The bare form
/// needs the pass itself — not just the yes/no [`check_accesspass`] gives — so it
/// can decide which legs the pass entitles.
fn require_accesspass<L: LedgerClient, W: Write>(
    ledger: &L,
    client_ip: Ipv4Addr,
    out: &mut W,
) -> eyre::Result<AccessPass> {
    match ledger.get_accesspass(client_ip, ledger.get_payer())? {
        Some(accesspass) => Ok(accesspass),
        None => {
            writeln!(
                out,
                "❌  Unable to find a valid AccessPass for the IP: {client_ip} UserPayer: {}",
                ledger.get_payer()
            )?;
            Err(eyre::eyre!(
                "A valid AccessPass is required to connect. Please contact support to obtain one."
            ))
        }
    }
}

/// Render the per-leg summary for a bare `doublezero connect` and decide its exit status.
///
/// A leg that failed names the command that re-runs just that leg, so a partial success
/// is recoverable without redoing the half that landed; the failed leg's report is
/// returned unchanged so `render_eyre` still shows the real cause. Mirrors the shape of
/// the partial-failure block in `execute_multicast_feeds`.
///
/// Provisioning nothing is an error even though no leg failed. Exiting 0 with nothing
/// connected is indistinguishable from success to an unattended installer — the same
/// reasoning that makes `auto_join_purchased_feeds` fail rather than no-op.
fn report_both<W: Write>(ibrl: LegOutcome, multicast: LegOutcome, out: &mut W) -> eyre::Result<()> {
    let both_failed = matches!(
        (&ibrl, &multicast),
        (LegOutcome::Failed(_), LegOutcome::Failed(_))
    );

    for (label, outcome, rerun, free_endpoint) in [
        (
            "IBRL",
            &ibrl,
            "doublezero connect ibrl",
            "doublezero disconnect multicast",
        ),
        (
            "Multicast",
            &multicast,
            "doublezero connect multicast",
            "doublezero disconnect ibrl",
        ),
    ] {
        match outcome {
            LegOutcome::Provisioned => writeln!(out, "    {label}: provisioned")?,
            LegOutcome::NoOp => writeln!(out, "    {label}: nothing authorized")?,
            LegOutcome::Skipped(reason) => writeln!(out, "⚠️  {label} skipped: {reason}")?,
            LegOutcome::Failed(e) => {
                writeln!(out, "❌  {label} failed: {e:#}")?;
                // A starved endpoint is not worth a rerun: the leg that succeeded is what
                // took the last one, so the next run recomputes the same exclusion and fails
                // identically. Name the way out instead — freeing an endpoint costs the other
                // tunnel, so it is offered as a choice, not as "the command that finishes".
                if e.chain().any(|cause| cause.is::<NoAvailableEndpoint>()) {
                    writeln!(
                        out,
                        "    Rerunning '{rerun}' will fail the same way. Free an endpoint with \
                         '{free_endpoint}', or connect from a machine that can reach another \
                         device."
                    )?;
                } else if !both_failed {
                    writeln!(out, "    Rerun '{rerun}' to finish.")?;
                }
            }
        }
    }

    let provisioned =
        matches!(ibrl, LegOutcome::Provisioned) || matches!(multicast, LegOutcome::Provisioned);
    let failed =
        matches!(ibrl, LegOutcome::Failed(_)) || matches!(multicast, LegOutcome::Failed(_));
    // Only when the command as a whole succeeded: this line is the terminal success
    // banner single-mode connect prints, and it would contradict both the `❌` above it
    // and the nonzero exit below if a leg failed. The per-leg lines already say what landed.
    if provisioned && !failed {
        writeln!(out, "✅  User Provisioned")?;
    }

    match (ibrl, multicast) {
        (LegOutcome::Failed(_), LegOutcome::Failed(_)) => {
            eyre::bail!("both legs failed; rerun the command")
        }
        (LegOutcome::Failed(e), _) | (_, LegOutcome::Failed(e)) => Err(e),
        _ if provisioned => Ok(()),
        _ => eyre::bail!(
            "nothing to connect: the AccessPass authorizes no unicast connection and no \
             multicast groups or feeds"
        ),
    }
}

/// The RFC-27 `user_type` a parsed mode will create a user as. The proof binds `user_type`, so a
/// mode needs a proof naming the kind of user it creates. Each parsed mode creates exactly one
/// kind; the bare form is the exception, running two legs, and builds a fetcher per leg instead
/// of going through here.
fn proof_user_type(mode: &ParsedDzMode) -> UserType {
    match mode {
        ParsedDzMode::Ibrl(user_type, _) => *user_type,
        ParsedDzMode::Multicast { .. } | ParsedDzMode::MulticastFeeds { .. } => UserType::Multicast,
    }
}

/// Obtains the invocation's RFC-27 proof, once, and only if a user is about to be created.
///
/// **Lazy on purpose.** A `connect` that finds its user already provisioned only re-activates it,
/// and an `--unsubscribe-feed`-only invocation creates nothing at all. Neither attaches a proof,
/// so neither may fail on one: fetching up front would let a verification problem take an
/// already-working host offline on a run that would never have used the answer. The fetch
/// therefore happens at the four points that actually call `create_user` /
/// `create_subscribe_user`, and nowhere else.
///
/// **Cached on purpose.** The proof binds `user_type`, and the several creation paths a multicast
/// `connect` can take all create the same kind of user, so they want one proof between them
/// rather than a request each. One fetcher therefore covers one `user_type`: a bare `doublezero
/// connect`, which provisions a unicast leg and a multicast leg, builds one for each.
///
/// A refusal is not fatal. The program is the enforcement point: without a proof, creation
/// succeeds while `require-ip-ownership-proof` is clear and fails with a named error once it is
/// set. Breaking `connect` here would take every host in a not-yet-enforcing environment offline
/// the moment a verifier went down.
///
/// The one hard failure is a proof for an address other than the one being provisioned. The
/// service signs what it observed; the daemon reports what it discovered. When those disagree the
/// host's verification request and its tunnel traffic leave by different paths, and neither
/// address is safe to use — attaching the proof guarantees an onchain rejection, and dropping it
/// binds an address nobody proved.
pub struct IpProofFetcher<'a> {
    client: &'a dyn IpProofClient,
    payer: Pubkey,
    user_type: UserType,
    client_ip: Ipv4Addr,
    /// The settled outcome, so the second creation path in one invocation reuses the first's
    /// answer instead of asking again. A mismatch is stored rather than raised so that it can be
    /// raised afresh at each creation site.
    outcome: std::sync::OnceLock<IpProofOutcome>,
}

/// What one request to the verification service settled on. Separate from `Result` because a
/// mismatch has to be re-raised at every creation site, and `eyre::Report` is not reusable.
#[derive(Clone, Copy, Debug)]
enum IpProofOutcome {
    /// A proof for the address being provisioned.
    Proof(IpOwnershipProof),
    /// No proof, for a reason already reported to the operator. Creation continues without one.
    None,
    /// A proof for some other address. `observed` is what the service saw.
    Mismatch { observed: Ipv4Addr },
}

impl<'a> IpProofFetcher<'a> {
    pub fn new(
        client: &'a dyn IpProofClient,
        payer: Pubkey,
        user_type: UserType,
        client_ip: Ipv4Addr,
    ) -> Self {
        Self {
            client,
            payer,
            user_type,
            client_ip,
            outcome: std::sync::OnceLock::new(),
        }
    }

    /// The proof to attach to a user this invocation is about to create, requesting it if this is
    /// the first creation path to ask. Errors only on an address mismatch.
    fn get<W: Write>(&self, out: &mut W) -> eyre::Result<Option<IpOwnershipProof>> {
        let outcome = match self.outcome.get() {
            Some(outcome) => *outcome,
            None => {
                let outcome = self.request(out)?;
                // A racing `set` cannot happen — `connect` is single-threaded — but preferring
                // the stored value keeps the cache authoritative either way.
                *self.outcome.get_or_init(|| outcome)
            }
        };

        match outcome {
            IpProofOutcome::Proof(proof) => Ok(Some(proof)),
            IpProofOutcome::None => Ok(None),
            IpProofOutcome::Mismatch { observed } => Err(self.mismatch_error(observed)),
        }
    }

    /// The request itself. Every non-mismatch failure is reported and swallowed.
    fn request<W: Write>(&self, out: &mut W) -> eyre::Result<IpProofOutcome> {
        let proof = match self
            .client
            .request_proof(self.payer, self.user_type, self.client_ip)
        {
            Ok(proof) => proof,
            Err(IpProofError::NotConfigured) => {
                tracing::debug!("no IP ownership verification service configured; continuing");
                return Ok(IpProofOutcome::None);
            }
            Err(err) => {
                writeln!(out, "⚠️  {err}")?;
                writeln!(
                    out,
                    "    Continuing without an IP ownership proof. This will be rejected onchain \
                     once IP ownership verification is required for this environment."
                )?;
                return Ok(IpProofOutcome::None);
            }
        };

        if proof.client_ip != self.client_ip {
            writeln!(
                out,
                "❌  The verification service observed this host at {}, but the daemon is \
                 provisioning {}.",
                proof.client_ip, self.client_ip
            )?;
            return Ok(IpProofOutcome::Mismatch {
                observed: proof.client_ip,
            });
        }

        writeln!(out, "    IP ownership verified for {}", self.client_ip)?;
        Ok(IpProofOutcome::Proof(proof))
    }

    fn mismatch_error(&self, observed: Ipv4Addr) -> eyre::Report {
        eyre::eyre!(
            "IP ownership verification saw {observed} while the daemon reports {}. The \
             verification request and the tunnel would leave this host by different paths. Set \
             --client-ip on the daemon to the address the tunnel must use, or point \
             --ip-verifier-url at a verifier reachable over that path.",
            self.client_ip
        )
    }
}

impl Connect {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        // The flag wins over the environment default, which in turn is overridable by
        // DZ_IP_VERIFIER_URL inside `config()`.
        let verifier_url = match &self.ip_verifier_url {
            Some(url) => Some(url.clone()),
            None => match ctx.env.config() {
                Ok(config) => config.ip_verifier_url,
                // Not fatal — no verifier means no proof, which is the same non-fatal path as an
                // environment that has none. But it must not look like that path: an operator who
                // exported DZ_IP_VERIFIER_URL and got no proof has to be able to tell a config
                // that failed to load from one that correctly names no verifier.
                Err(err) => {
                    tracing::warn!(
                        error = %err,
                        "could not load the environment config to find the IP ownership \
                         verification service; continuing as though none is configured"
                    );
                    None
                }
            },
        };
        let proof_client = HttpIpProofClient::new(verifier_url);
        self.execute_with_proof_client(ctx, daemon, ledger, &proof_client, out)
            .await
    }

    /// The body of [`Connect::execute`], with the verification service abstracted so tests can
    /// drive every branch without an HTTP server.
    pub async fn execute_with_proof_client<
        D: DaemonClient,
        L: LedgerClient,
        P: IpProofClient,
        W: Write,
    >(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        proof_client: &P,
        out: &mut W,
    ) -> eyre::Result<()> {
        // The bare form runs both legs, so it ticks through roughly twice the steps.
        let spinner = init_spinner(if self.dz_mode.is_none() { 9 } else { 5 });

        // Check that we have a keypair + balance, and that the daemon is
        // reachable and on the same environment as the client.
        ledger.check_requirements()?;
        check_daemon(daemon, ledger).await?;

        writeln!(out, "⚡  Connecting to {}...", ledger.get_environment())?;

        // Deprecation warning for --client-ip flag
        if self.client_ip.is_some() {
            writeln!(
                out,
                "⚠️  WARNING: --client-ip on the CLI is deprecated and will be ignored. \
                 Set --client-ip on the daemon (doublezerod) instead."
            )?;
        }

        // Get public IP from daemon
        let client_ip = resolve_client_ip(daemon).await?;
        let client_ip_str = client_ip.to_string();

        // No subcommand: provision everything the AccessPass authorizes.
        if self.dz_mode.is_none() {
            let res = self
                .execute_both(ledger, daemon, client_ip, proof_client, &spinner, out)
                .await;
            spinner.finish_and_clear();
            return res;
        }

        let parsed_mode = self.parse_dz_mode()?;
        // Multicast users are not subject to epoch expiry — only verify the AccessPass exists.
        let enforce_epoch = !matches!(
            parsed_mode,
            ParsedDzMode::Multicast { .. } | ParsedDzMode::MulticastFeeds { .. }
        );

        if !check_accesspass(ledger, client_ip, enforce_epoch)? {
            writeln!(
                out,
                "❌  Unable to find a valid AccessPass for the IP: {client_ip_str} UserPayer: {}",
                ledger.get_payer()
            )?;
            return Err(eyre::eyre!(
                "A valid AccessPass is required to connect. Please contact support to obtain one."
            ));
        }

        spinner.inc(1);
        writeln!(out, "    DoubleZero ID: {}", ledger.get_payer())?;
        writeln!(out, "⚡  Provisioning for IP: {client_ip_str}")?;

        // RFC-27: one proof for this mode, which creates users of a single `user_type`. (The
        // bare form runs two legs and builds one per leg; it returned above.) Nothing is
        // requested here — see [`IpProofFetcher`] for why the request waits until a creation
        // path asks for it.
        let ip_proof = IpProofFetcher::new(
            proof_client,
            ledger.get_payer(),
            proof_user_type(&parsed_mode),
            client_ip,
        );
        let ip_proof = &ip_proof;

        let provisioned = match parsed_mode {
            ParsedDzMode::Ibrl(user_type, tenant) => {
                self.execute_ibrl(
                    ledger, daemon, user_type, client_ip, tenant, ip_proof, &spinner, out,
                )
                .await?;
                true
            }
            ParsedDzMode::Multicast {
                pub_groups,
                sub_groups,
            } => {
                self.execute_multicast(
                    ledger,
                    daemon,
                    &pub_groups,
                    &sub_groups,
                    client_ip,
                    ip_proof,
                    &spinner,
                    out,
                )
                .await?
            }
            ParsedDzMode::MulticastFeeds {
                sub_feeds,
                unsub_feeds,
            } => {
                self.execute_multicast_feeds(
                    ledger,
                    daemon,
                    &sub_feeds,
                    &unsub_feeds,
                    client_ip,
                    ip_proof,
                    &spinner,
                    out,
                )
                .await?
            }
        };

        if provisioned {
            writeln!(out, "✅  User Provisioned")?;
        }
        spinner.finish_and_clear();

        Ok(())
    }

    /// Bare `doublezero connect`: provision every mode the caller's AccessPass authorizes.
    ///
    /// The provisioning itself is entirely the existing legs — `execute_ibrl`, and
    /// `execute_multicast` with empty group slices, which already means "join every
    /// group this pass allows" and covers the EdgeSeat purchased-feed path. What is new
    /// here is the entitlement gating, enabling the reconciler, and per-leg reporting.
    ///
    /// IBRL runs first on purpose: the multicast leg re-reads `list_user()`, so it sees
    /// the fresh unicast user and places the multicast user on a *different* device
    /// (RFC-13 concurrent tunnels). Both legs are attempted even when one fails, so a
    /// second-leg failure never discards the first leg's work.
    async fn execute_both<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        client_ip: Ipv4Addr,
        proof_client: &dyn IpProofClient,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        let accesspass = require_accesspass(ledger, client_ip, out)?;

        spinner.inc(1);
        writeln!(out, "    DoubleZero ID: {}", ledger.get_payer())?;
        writeln!(out, "⚡  Provisioning for IP: {client_ip}")?;

        // Enable up front rather than leaving it to whichever leg reaches
        // `user_activated` first: a run where every leg skips must still leave the
        // daemon managing tunnels. Non-fatal, matching `user_activated` — the onchain
        // users are still worth creating if only the local daemon is uncooperative.
        match ensure_reconciler_enabled(daemon).await {
            Ok(true) => {}
            Ok(false) => writeln!(out, "    Reconciler enabled")?,
            Err(e) => writeln!(
                out,
                "⚠️  Failed to enable the reconciler: {e}. Tunnels will not be provisioned \
                 until you run 'doublezero enable'."
            )?,
        }

        // Which users already exist decides whether a seat cap even applies: the program
        // ticks a seat in `try_add_user` only on create, so a pass whose seats are full
        // must still re-run cleanly for the user already holding one.
        let users = ledger.list_user()?;
        let ibrl_exists = users.values().any(|u| {
            u.client_ip == client_ip
                && matches!(u.user_type, UserType::IBRL | UserType::IBRLWithAllocatedIP)
        });
        let mcast_exists = users
            .values()
            .any(|u| u.client_ip == client_ip && u.user_type == UserType::Multicast);
        // Seat caps are EdgeSeat-only; `try_add_user` returns Ok immediately otherwise.
        let edge_seat = matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_));

        let epoch = ledger.get_epoch()?;
        let ibrl = if accesspass.last_access_epoch < epoch {
            LegOutcome::Skipped(format!(
                "the AccessPass expired at epoch {} (current {epoch}); multicast is not \
                 epoch-gated, so it was still attempted",
                accesspass.last_access_epoch
            ))
        } else if edge_seat
            && !ibrl_exists
            && accesspass.unicast_user_count >= accesspass.max_unicast_users
        {
            LegOutcome::Skipped(format!(
                "the EdgeSeat AccessPass has no free unicast seat ({} of {} in use)",
                accesspass.unicast_user_count, accesspass.max_unicast_users
            ))
        } else {
            let user_type = if self.allocate_addr {
                UserType::IBRLWithAllocatedIP
            } else {
                UserType::IBRL
            };
            // A proof per leg, not per invocation: the proof binds `user_type`, and this form
            // creates a unicast user and a multicast one. A single proof would be refused
            // onchain by whichever leg it did not name.
            let ip_proof =
                IpProofFetcher::new(proof_client, ledger.get_payer(), user_type, client_ip);
            match self
                .execute_ibrl(
                    ledger,
                    daemon,
                    user_type,
                    client_ip,
                    self.tenant.clone(),
                    &ip_proof,
                    spinner,
                    out,
                )
                .await
            {
                Ok(()) => LegOutcome::Provisioned,
                Err(e) => LegOutcome::Failed(e),
            }
        };

        // No allowlist/feed pre-check: `execute_multicast` already reports an empty
        // allowlist itself, and its EdgeSeat path diagnoses full seats and wrong metros
        // far better than a gate here could.
        let multicast = if edge_seat
            && !mcast_exists
            && accesspass.multicast_user_count >= accesspass.max_multicast_users
        {
            LegOutcome::Skipped(format!(
                "the EdgeSeat AccessPass has no free multicast seat ({} of {} in use)",
                accesspass.multicast_user_count, accesspass.max_multicast_users
            ))
        } else {
            let ip_proof = IpProofFetcher::new(
                proof_client,
                ledger.get_payer(),
                UserType::Multicast,
                client_ip,
            );
            match self
                .execute_multicast(ledger, daemon, &[], &[], client_ip, &ip_proof, spinner, out)
                .await
            {
                Ok(true) => LegOutcome::Provisioned,
                Ok(false) => LegOutcome::NoOp,
                Err(e) => LegOutcome::Failed(e),
            }
        };

        report_both(ibrl, multicast, out)
    }

    #[allow(clippy::too_many_arguments)]
    async fn execute_ibrl<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        user_type: UserType,
        client_ip: Ipv4Addr,
        tenant: Option<String>,
        ip_proof: &IpProofFetcher<'_>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Look for user
        let (_user_pubkey, user) = self
            .find_or_create_user(
                ledger, daemon, &client_ip, spinner, user_type, tenant, ip_proof, out,
            )
            .await?;

        // Check user status
        match user.status {
            UserStatus::Activated => {
                self.user_activated(daemon, user_type, spinner, out).await?;
                Ok(())
            }
            _ => eyre::bail!("User status not expected"),
        }
    }

    #[allow(clippy::too_many_arguments)]
    async fn execute_multicast<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        pub_groups: &[String],
        sub_groups: &[String],
        client_ip: Ipv4Addr,
        ip_proof: &IpProofFetcher<'_>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<bool> {
        let mcast_groups = ledger.list_multicastgroup()?;

        let (pub_group_pks, sub_group_pks) = if pub_groups.is_empty() && sub_groups.is_empty() {
            // No groups specified: auto-join every group authorized in the caller's
            // AccessPass — publish to its publisher allowlist and subscribe to its
            // subscriber allowlist. The pass is guaranteed to exist (validated by
            // check_accesspass before dispatch); the ok_or_else is defensive.
            let accesspass = ledger
                .get_accesspass(client_ip, ledger.get_payer())?
                .ok_or_else(|| {
                    eyre::eyre!(
                        "No valid AccessPass found for IP: {} user_payer: {}",
                        client_ip,
                        ledger.get_payer()
                    )
                })?;

            // A pass carrying purchased feeds gates access through the Feed account, not the
            // multicast allowlists, which stay empty on it. Selecting its feeds here is what
            // makes a zero-argument connect work for a feed customer.
            if !accesspass.feed_seats().is_empty() {
                return self
                    .auto_join_purchased_feeds(
                        ledger,
                        daemon,
                        &accesspass,
                        client_ip,
                        ip_proof,
                        spinner,
                        out,
                    )
                    .await;
            }

            // Keep only allowlist entries that still resolve to a known group; drop
            // pubkeys left over from deleted groups.
            let pub_group_pks: Vec<Pubkey> = accesspass
                .mgroup_pub_allowlist
                .iter()
                .filter(|pk| mcast_groups.contains_key(pk))
                .copied()
                .collect();
            let sub_group_pks: Vec<Pubkey> = accesspass
                .mgroup_sub_allowlist
                .iter()
                .filter(|pk| mcast_groups.contains_key(pk))
                .copied()
                .collect();

            if pub_group_pks.is_empty() && sub_group_pks.is_empty() {
                writeln!(
                    out,
                    "ℹ️  The AccessPass has no authorized multicast groups; nothing to connect to."
                )?;
                return Ok(false);
            }

            let code_of = |pk: &Pubkey| {
                mcast_groups
                    .get(pk)
                    .map(|g| g.code.clone())
                    .unwrap_or_else(|| pk.to_string())
            };
            if !pub_group_pks.is_empty() {
                let codes: Vec<String> = pub_group_pks.iter().map(code_of).collect();
                writeln!(
                    out,
                    "    Publishing to (from AccessPass): {}",
                    codes.join(", ")
                )?;
            }
            if !sub_group_pks.is_empty() {
                let codes: Vec<String> = sub_group_pks.iter().map(code_of).collect();
                writeln!(
                    out,
                    "    Subscribing to (from AccessPass): {}",
                    codes.join(", ")
                )?;
            }

            (pub_group_pks, sub_group_pks)
        } else {
            // Resolve pub group codes to pubkeys
            let mut pub_group_pks = Vec::new();
            for group_code in pub_groups {
                let (pk, _) = mcast_groups
                    .iter()
                    .find(|(_, g)| g.code == *group_code)
                    .ok_or_else(|| eyre::eyre!("Multicast group not found: {}", group_code))?;
                if pub_group_pks.contains(pk) {
                    eyre::bail!("Duplicate multicast pub group: {}", group_code);
                }
                pub_group_pks.push(*pk);
            }

            // Resolve sub group codes to pubkeys
            let mut sub_group_pks = Vec::new();
            for group_code in sub_groups {
                let (pk, _) = mcast_groups
                    .iter()
                    .find(|(_, g)| g.code == *group_code)
                    .ok_or_else(|| eyre::eyre!("Multicast group not found: {}", group_code))?;
                if sub_group_pks.contains(pk) {
                    eyre::bail!("Duplicate multicast sub group: {}", group_code);
                }
                sub_group_pks.push(*pk);
            }

            (pub_group_pks, sub_group_pks)
        };

        // Look for user and subscribe to all groups
        let (_user_pubkey, user) = self
            .find_or_create_user_and_subscribe(
                ledger,
                daemon,
                &client_ip,
                spinner,
                &pub_group_pks,
                &sub_group_pks,
                ip_proof,
                out,
            )
            .await?;

        match user.status {
            UserStatus::Activated => {
                self.user_activated(daemon, UserType::Multicast, spinner, out)
                    .await?;
                Ok(true)
            }
            _ => eyre::bail!("User status not expected"),
        }
    }

    /// `--subscribe-feed` / `--unsubscribe-feed`: join or leave whole feeds on an EdgeSeat pass.
    ///
    /// Both flags are validated fully before anything is sent, so a deterministic rejection
    /// (unknown feed, wrong metro, not held, overlap) leaves the chain untouched and never
    /// strands a half-done swap. When both flags are given the leave runs first, freeing its
    /// slot for a swap at the per-user feed cap; a failure after validation is a transaction
    /// failure, so "rerun the failed flag" is real advice.
    #[allow(clippy::too_many_arguments)]
    async fn execute_multicast_feeds<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        sub_feeds: &[String],
        unsub_feeds: &[String],
        client_ip: Ipv4Addr,
        ip_proof: &IpProofFetcher<'_>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<bool> {
        let feeds = ledger.list_feed()?;
        let users = ledger.list_user()?;
        let mcast_user = users
            .iter()
            .find(|(_, u)| u.client_ip == client_ip && u.user_type == UserType::Multicast)
            .map(|(pk, u)| (*pk, u.clone()));

        let unsub_pks: Vec<Pubkey> = if unsub_feeds.is_empty() {
            vec![]
        } else {
            let Some((_, user)) = &mcast_user else {
                eyre::bail!("no Multicast user exists for IP {client_ip}; nothing to leave");
            };
            resolve_held_feeds(unsub_feeds, &feeds, user)?
        };

        let feed_join = if sub_feeds.is_empty() {
            None
        } else {
            Some(
                self.resolve_feed_join(
                    ledger,
                    daemon,
                    mcast_user.as_ref(),
                    &feeds,
                    sub_feeds,
                    &unsub_pks,
                    client_ip,
                    spinner,
                    out,
                )
                .await?,
            )
        };

        // Catches a code in one flag naming the same feed as a pubkey (or another casing) in the
        // other; the raw-string check in parse_dz_mode only catches identical spellings.
        if let Some(join) = &feed_join {
            if let Some(pk) = join.feed_pks.iter().find(|pk| unsub_pks.contains(pk)) {
                eyre::bail!("feed {pk} is in both --subscribe-feed and --unsubscribe-feed");
            }
        }

        let unsub_result = if unsub_pks.is_empty() {
            None
        } else {
            let (user_pk, _) = mcast_user.as_ref().expect("checked during validation");
            spinner.set_message("Leaving feed(s)...");
            let result = ledger.unsubscribe_feed(UnsubscribeFeedCommand {
                user_pk: *user_pk,
                feed_pks: unsub_pks,
            });
            if result.is_ok() {
                writeln!(out, "    Left feed(s): {}", unsub_feeds.join(", "))?;
            }
            Some(result)
        };

        let sub_result = match feed_join {
            None => None,
            Some(join) => {
                let result =
                    self.execute_feed_join(ledger, join, client_ip, ip_proof, spinner, out);
                if result.is_ok() {
                    writeln!(out, "    Joined feed(s): {}", sub_feeds.join(", "))?;
                }
                Some(result)
            }
        };

        match (unsub_result, sub_result) {
            (Some(Err(unsub_err)), Some(Ok(()))) => {
                // The join landed, so still hand the daemon its provisioning work.
                self.user_activated(daemon, UserType::Multicast, spinner, out)
                    .await?;
                writeln!(out, "❌  --unsubscribe-feed failed: {unsub_err:#}")?;
                writeln!(
                    out,
                    "    --subscribe-feed succeeded. Rerun with only --unsubscribe-feed {} to finish.",
                    unsub_feeds.join(" ")
                )?;
                Err(unsub_err)
            }
            (Some(Ok(())), Some(Err(sub_err))) => {
                writeln!(out, "❌  --subscribe-feed failed: {sub_err:#}")?;
                writeln!(
                    out,
                    "    --unsubscribe-feed succeeded. Rerun with only --subscribe-feed {} to finish.",
                    sub_feeds.join(" ")
                )?;
                Err(sub_err)
            }
            (Some(Err(unsub_err)), Some(Err(sub_err))) => {
                writeln!(out, "❌  --unsubscribe-feed failed: {unsub_err:#}")?;
                writeln!(out, "❌  --subscribe-feed failed: {sub_err:#}")?;
                eyre::bail!("both halves failed; rerun the command");
            }
            (Some(Err(err)), None) | (None, Some(Err(err))) => Err(err),
            (_, Some(Ok(()))) => {
                self.user_activated(daemon, UserType::Multicast, spinner, out)
                    .await?;
                Ok(true)
            }
            // A pure leave changes routes only; the daemon reconciler picks that up on its own.
            _ => Ok(false),
        }
    }

    /// Bare `connect multicast` on a pass carrying purchased feeds: pick the device the way a
    /// bare connect always has, then join every purchased feed in that device's metro that still
    /// has a free seat.
    ///
    /// One machine can only ever hold one metro's feeds, because a user account is keyed on
    /// `(client_ip, user_type)` with no device — so out-of-metro feeds are reported, never
    /// silently dropped. Failing when this machine holds nothing and could take nothing is
    /// deliberate: exiting 0 with nothing subscribed is indistinguishable from success to an
    /// unattended installer. A re-run that holds a feed already still activates the user: a
    /// disabled reconciler must not go unnoticed just because nothing new needed joining.
    #[allow(clippy::too_many_arguments)]
    async fn auto_join_purchased_feeds<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        accesspass: &AccessPass,
        client_ip: Ipv4Addr,
        ip_proof: &IpProofFetcher<'_>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<bool> {
        let feeds = ledger.list_feed()?;
        let users = ledger.list_user()?;
        // Cosmetic only, for the metro names in the messages below: a device code
        // conventionally leads with the metro, but that is a naming convention, not data, so a
        // failure here must degrade to bare codes, never fail the connect.
        let exchanges = ledger.list_exchange().unwrap_or_default();
        let mcast_user = users
            .iter()
            .find(|(_, u)| u.client_ip == client_ip && u.user_type == UserType::Multicast)
            .map(|(pk, u)| (*pk, u.clone()));

        // Defined before device selection: none of these depend on which device gets chosen, and
        // the pre-device capacity check below needs `describe_full` too.
        let seat_of = |feed_pk: &Pubkey| {
            accesspass
                .feed_seats()
                .iter()
                .find(|seat| seat.feed_key == *feed_pk)
                .map(|seat| (seat.current_users, seat.max_users))
                .unwrap_or((0, 0))
        };
        let code_of = |feed_pk: &Pubkey| feed_code_or_pubkey(feed_pk, &feeds);
        let describe_full = |feed_pks: &[Pubkey]| {
            feed_pks
                .iter()
                .map(|pk| {
                    let (current_users, max_users) = seat_of(pk);
                    format!(
                        "{} ({current_users} of {max_users} seats in use)",
                        code_of(pk)
                    )
                })
                .collect::<Vec<String>>()
                .join(", ")
        };

        let (join_user, device, held_feed_pks) = match &mcast_user {
            Some((pk, user)) => {
                let device = ledger
                    .get_device(user.device_pk.to_string())
                    .map_err(|err| {
                        eyre::eyre!("failed to fetch device {}: {err}", user.device_pk)
                    })?;
                writeln!(out, "    An account already exists with Pubkey: {pk}")?;
                writeln!(out, "    Device selected: {}", device.code)?;
                // The user's device is fixed; a --device naming another one is a no-op the
                // operator should hear about rather than have silently ignored.
                if let Some(requested) = self.device.as_ref() {
                    if *requested != user.device_pk.to_string() && *requested != device.code {
                        eyre::bail!(
                            "existing Multicast user {pk} is on device {}; run 'doublezero disconnect multicast' first, or omit --device",
                            device.code
                        );
                    }
                }
                (FeedJoinUser::Existing(*pk), device, user.feed_pks.clone())
            }
            None => {
                let mut devices = ledger.list_device()?;
                let mut excluded_by_metro: HashMap<Pubkey, Device> = HashMap::new();
                let mut devices_before_metro_filter = None;
                if self.device.is_none() {
                    devices.retain(|_, d| {
                        d.is_device_eligible_for_provisioning()
                            && d.check_user_type_capacity(UserType::Multicast, false)
                                .is_none()
                    });

                    // Only devices in a metro a purchased feed with headroom actually serves can
                    // ever admit this join — check_feed_metro_coverage enforces that onchain, so
                    // latency alone must not land the customer somewhere no purchased feed can
                    // follow. Full seats are excluded from `candidate_exchanges` here (not just
                    // filtered later): otherwise latency could still land the customer in a metro
                    // where every purchased feed there is full while another metro had a free
                    // seat. Built only here, under this branch: unread when `--device` is set.
                    let mut candidate_exchanges: Vec<Pubkey> = Vec::new();
                    let mut candidate_feed_pks: Vec<Pubkey> = Vec::new();
                    let mut full: Vec<Pubkey> = Vec::new();
                    for seat in feed_seats_by_code(accesspass.feed_seats(), &feeds) {
                        let Some(feed) = feeds.get(&seat.feed_key) else {
                            continue;
                        };
                        if seat.current_users >= seat.max_users {
                            full.push(seat.feed_key);
                            continue;
                        }
                        candidate_feed_pks.push(seat.feed_key);
                        if !candidate_exchanges.contains(&feed.exchange) {
                            candidate_exchanges.push(feed.exchange);
                        }
                    }

                    if candidate_exchanges.is_empty() {
                        // No purchased feed has headroom, so no device could ever help; fail
                        // now with the same wording the post-device path below would give.
                        if !full.is_empty() {
                            eyre::bail!(
                                "every purchased feed is already at capacity: {}. Free a seat by disconnecting another machine, or buy more.",
                                describe_full(&full)
                            );
                        }
                        eyre::bail!(
                            "the access pass carries {} purchased feed(s) but none could be joined: 0 over this machine's feed limit of {MAX_USER_FEEDS}, {} feed account(s) not found",
                            accesspass.feed_seats().len(),
                            accesspass.feed_seats().len()
                        );
                    }

                    // Zero eligible devices is a device-availability problem unrelated to metro
                    // coverage; let find_or_create_device's own error (via best_latency) surface
                    // below rather than masking it behind a metro-specific message.
                    if !devices.is_empty() {
                        devices_before_metro_filter = Some(devices.clone());
                        for (pk, candidate_device) in devices.iter() {
                            if !candidate_exchanges.contains(&candidate_device.exchange_pk) {
                                excluded_by_metro.insert(*pk, candidate_device.clone());
                            }
                        }
                        devices.retain(|_, d| candidate_exchanges.contains(&d.exchange_pk));
                        if devices.is_empty() {
                            let candidate_descriptions: Vec<String> = candidate_feed_pks
                                .iter()
                                .map(|pk| feed_metro_description(pk, &feeds, &exchanges))
                                .collect();
                            let destination = destination_clause(&candidate_exchanges, &exchanges);
                            if full.is_empty() {
                                eyre::bail!(
                                    "no eligible device serves the metro of purchased feed(s) with a free seat: {}. Connect from a machine in {destination}.",
                                    candidate_descriptions.join(", ")
                                );
                            }
                            eyre::bail!(
                                "no eligible device serves the metro of purchased feed(s) with a free seat: {}; also already at capacity: {}. Connect from a machine in {destination}.",
                                candidate_descriptions.join(", "),
                                describe_full(&full)
                            );
                        }
                    }
                }
                let exclude_ips = exclude_ips(&users, &client_ip, &devices);
                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(ledger, daemon, &devices, spinner, &exclude_ips)
                    .await?;
                writeln!(out, "    Device selected: {}", device.code)?;
                if let Some(err_msg) = device.check_user_type_capacity(UserType::Multicast, false) {
                    return Err(eyre::eyre!(err_msg));
                }

                // Informational only: routing through the feeds' metro instead of the nearest
                // device can leave a faster device excluded, which is an upsell signal, not a
                // missed option — the wording must never imply the excluded device could have
                // carried this connection today. Fetched only now, on the success path, and only
                // when something was actually excluded: an advisory must cost nothing on a path
                // that fails, and nothing when there is nothing to compare against.
                if !excluded_by_metro.is_empty() {
                    if let Some(devices_before_metro_filter) = &devices_before_metro_filter {
                        if let Ok(latencies) = retrieve_latencies(
                            daemon,
                            devices_before_metro_filter,
                            true,
                            Some(spinner),
                        )
                        .await
                        {
                            if let Some(chosen) = latencies
                                .iter()
                                .find(|latency| latency.device_pk == device_pk.to_string())
                            {
                                let faster_excluded = excluded_by_metro
                                    .iter()
                                    .filter_map(|(pk, excluded_device)| {
                                        latencies
                                            .iter()
                                            .find(|latency| latency.device_pk == pk.to_string())
                                            .map(|latency| (excluded_device, latency))
                                    })
                                    .min_by(|(left_device, left_latency), (right_device, right_latency)| {
                                        compare_latency_records(left_latency, right_latency)
                                            .then_with(|| left_device.code.cmp(&right_device.code))
                                    });
                                if let Some((excluded_device, excluded_latency)) = faster_excluded {
                                    if chosen.avg_latency_ns - excluded_latency.avg_latency_ns
                                        > LOWER_LATENCY_NOTICE_THRESHOLD_NS
                                    {
                                        let device_label = match metro_name(
                                            &excluded_device.exchange_pk,
                                            &exchanges,
                                        ) {
                                            Some(metro) => {
                                                format!("{} in {metro}", excluded_device.code)
                                            }
                                            None => excluded_device.code.clone(),
                                        };
                                        writeln!(
                                            out,
                                            "ℹ️  Lower latency is available from {device_label} ({} vs {})",
                                            format_latency_ms(excluded_latency.avg_latency_ns),
                                            format_latency_ms(chosen.avg_latency_ns),
                                        )?;
                                    }
                                }
                            }
                        }
                    }
                }

                (
                    FeedJoinUser::Create {
                        device_pk,
                        tunnel_endpoint,
                    },
                    device,
                    vec![],
                )
            }
        };

        let selection = select_purchased_feeds(
            accesspass.feed_seats(),
            &feeds,
            &device.exchange_pk,
            &held_feed_pks,
        );

        if !selection.held.is_empty() {
            let codes: Vec<String> = selection.held.iter().map(code_of).collect();
            writeln!(out, "    Already joined: {}", codes.join(", "))?;
        }
        if !selection.full.is_empty() {
            writeln!(
                out,
                "    Skipped, no free seat: {}",
                describe_full(&selection.full)
            )?;
        }
        if !selection.other_metro.is_empty() {
            // Bare codes only: the header already says "another metro" and the device selected
            // a moment ago is two lines above, so repeating the comparison per feed adds nothing.
            let codes: Vec<String> = selection.other_metro.iter().map(code_of).collect();
            writeln!(out, "    Skipped, another metro: {}", codes.join(", "))?;
        }
        if !selection.over_feed_limit.is_empty() {
            let codes: Vec<String> = selection.over_feed_limit.iter().map(code_of).collect();
            writeln!(
                out,
                "    Skipped, would exceed this machine's feed limit of {MAX_USER_FEEDS}: {}",
                codes.join(", ")
            )?;
        }
        if !selection.unknown.is_empty() {
            let keys: Vec<String> = selection.unknown.iter().map(|pk| pk.to_string()).collect();
            writeln!(
                out,
                "    Skipped, feed account not found: {}",
                keys.join(", ")
            )?;
        }

        if selection.join.is_empty() {
            // Already holding a feed means this machine is provisioned; joining nothing is then a
            // successful re-run, not a failure. Still activate: a re-run whose reconciler is
            // disabled must not exit 0 while leaving the tunnel unprovisioned.
            if !selection.held.is_empty() {
                self.user_activated(daemon, UserType::Multicast, spinner, out)
                    .await?;
                return Ok(true);
            }
            if !selection.full.is_empty() {
                eyre::bail!(
                    "every purchased feed is already at capacity: {}. Free a seat by disconnecting another machine, or buy more.",
                    describe_full(&selection.full)
                );
            }
            if !selection.other_metro.is_empty() {
                // The full per-feed comparison lives only here: this is the one place it appears.
                let described: Vec<String> = selection
                    .other_metro
                    .iter()
                    .map(|pk| feed_metro_description(pk, &feeds, &exchanges))
                    .collect();
                let other_metro_exchanges: Vec<Pubkey> = selection
                    .other_metro
                    .iter()
                    .filter_map(|pk| feeds.get(pk).map(|feed| feed.exchange))
                    .collect();
                let destination = destination_clause(&other_metro_exchanges, &exchanges);
                eyre::bail!(
                    "no purchased feed serves the metro of device {}: {}. Connect from a machine in {destination}.",
                    device.code,
                    described.join(", ")
                );
            }
            eyre::bail!(
                "the access pass carries {} purchased feed(s) but none could be joined: {} over this machine's feed limit of {MAX_USER_FEEDS}, {} feed account(s) not found",
                accesspass.feed_seats().len(),
                selection.over_feed_limit.len(),
                selection.unknown.len()
            );
        }

        let codes: Vec<String> = selection.join.iter().map(code_of).collect();
        self.execute_feed_join(
            ledger,
            FeedJoin {
                user: join_user,
                feed_pks: selection.join,
            },
            client_ip,
            ip_proof,
            spinner,
            out,
        )?;
        writeln!(out, "    Joined feed(s): {}", codes.join(", "))?;
        self.user_activated(daemon, UserType::Multicast, spinner, out)
            .await?;
        Ok(true)
    }

    /// Resolve everything the join needs without sending anything: the device (the existing
    /// user's, or the best one for a new user), the feed names against that device's metro, and
    /// whether a multicast user already exists or one must be created.
    #[allow(clippy::too_many_arguments)]
    async fn resolve_feed_join<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        mcast_user: Option<&(Pubkey, User)>,
        feeds: &HashMap<Pubkey, Feed>,
        names: &[String],
        leaving: &[Pubkey],
        client_ip: Ipv4Addr,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<FeedJoin> {
        let (join, held) = match mcast_user {
            Some((pk, user)) => {
                let device = ledger
                    .get_device(user.device_pk.to_string())
                    .map_err(|err| {
                        eyre::eyre!("failed to fetch device {}: {err}", user.device_pk)
                    })?;
                writeln!(out, "    An account already exists with Pubkey: {pk}")?;
                // The user's device is fixed; a --device naming another one is a no-op the
                // operator should hear about rather than have silently ignored.
                if let Some(requested) = self.device.as_ref() {
                    if *requested != user.device_pk.to_string() && *requested != device.code {
                        eyre::bail!(
                            "existing Multicast user {pk} is on device {}; run 'doublezero disconnect multicast' first, or omit --device",
                            device.code
                        );
                    }
                }
                let feed_pks = resolve_feeds_for_metro(
                    names,
                    feeds,
                    &device,
                    "run 'doublezero disconnect multicast' first to reconnect on a device in the feed's metro",
                )?;
                (
                    FeedJoin {
                        user: FeedJoinUser::Existing(*pk),
                        feed_pks,
                    },
                    user.feed_pks.clone(),
                )
            }
            None => {
                // The feeds choose the metro: a code may exist in several, a pubkey names one.
                // Every requested feed must share a metro, since one device carries the tunnel.
                let mut allowed_exchanges: Option<Vec<Pubkey>> = None;
                for name in names {
                    let exchanges: Vec<Pubkey> = match name.parse::<Pubkey>() {
                        Ok(pk) => match feeds.get(&pk) {
                            Some(feed) => vec![feed.exchange],
                            None => eyre::bail!("feed {name} not found"),
                        },
                        Err(_) => {
                            let exchanges: Vec<Pubkey> = feeds
                                .values()
                                .filter(|f| f.code == *name)
                                .map(|f| f.exchange)
                                .collect();
                            if exchanges.is_empty() {
                                eyre::bail!("feed {name} not found");
                            }
                            exchanges
                        }
                    };
                    allowed_exchanges = Some(match allowed_exchanges {
                        None => exchanges,
                        Some(prev) => {
                            let shared: Vec<Pubkey> = prev
                                .into_iter()
                                .filter(|exchange| exchanges.contains(exchange))
                                .collect();
                            if shared.is_empty() {
                                eyre::bail!("the requested feeds do not share a metro; one device carries the tunnel, so join them separately");
                            }
                            shared
                        }
                    });
                }
                let allowed_exchanges = allowed_exchanges.expect("names is not empty");

                let users = ledger.list_user()?;
                let mut devices = ledger.list_device()?;
                if self.device.is_none() {
                    devices.retain(|_, d| {
                        allowed_exchanges.contains(&d.exchange_pk)
                            && d.is_device_eligible_for_provisioning()
                            && d.check_user_type_capacity(UserType::Multicast, false)
                                .is_none()
                    });
                    if devices.is_empty() {
                        eyre::bail!("no eligible device serves the metro of the requested feed(s)");
                    }
                }
                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, &client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(ledger, daemon, &devices, spinner, &exclude_ips)
                    .await?;
                writeln!(out, "    Device selected: {}", device.code)?;

                if let Some(err_msg) = device.check_user_type_capacity(UserType::Multicast, false) {
                    return Err(eyre::eyre!(err_msg));
                }

                // Auto-selection filtered to the feeds' metros, so this only fails when the
                // operator passed a --device in another one.
                let feed_pks = resolve_feeds_for_metro(
                    names,
                    feeds,
                    &device,
                    "omit --device to pick a device in the feed's metro automatically",
                )?;
                (
                    FeedJoin {
                        user: FeedJoinUser::Create {
                            device_pk,
                            tunnel_endpoint,
                        },
                        feed_pks,
                    },
                    vec![],
                )
            }
        };

        // Refuse here what the program would refuse after the create: by then the bare user
        // would already exist, holding a multicast slot and a device seat for nothing.
        let accesspass = ledger
            .get_accesspass(client_ip, ledger.get_payer())?
            .ok_or_else(|| {
                eyre::eyre!(
                    "No valid AccessPass found for IP: {client_ip} user_payer: {}",
                    ledger.get_payer()
                )
            })?;
        if !matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_)) {
            eyre::bail!(
                "the access pass is {}; only an EdgeSeat pass carries feeds",
                accesspass.accesspass_type
            );
        }
        for feed_pk in &join.feed_pks {
            if !accesspass
                .feed_seats()
                .iter()
                .any(|seat| seat.feed_key == *feed_pk)
            {
                eyre::bail!("feed {feed_pk} is not provisioned on the access pass");
            }
        }
        // The cap applies to the state the join will see: the leave half runs first, so feeds
        // being left do not count as held.
        let remaining: Vec<Pubkey> = held
            .iter()
            .filter(|pk| !leaving.contains(pk))
            .copied()
            .collect();
        let new_feeds = join
            .feed_pks
            .iter()
            .filter(|pk| !remaining.contains(pk))
            .count();
        if remaining.len() + new_feeds > MAX_USER_FEEDS {
            eyre::bail!(
                "this join would leave the user holding {} feeds; a user may hold at most {MAX_USER_FEEDS}",
                remaining.len() + new_feeds
            );
        }

        Ok(join)
    }

    /// Execute a validated join: create the bare user when needed (CreateUser is idempotent, so a
    /// retry is safe), poll it to Activated, and send the SubscribeFeed.
    fn execute_feed_join<L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        join: FeedJoin,
        client_ip: Ipv4Addr,
        ip_proof: &IpProofFetcher<'_>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        let user_pk = match join.user {
            FeedJoinUser::Existing(pk) => pk,
            FeedJoinUser::Create {
                device_pk,
                tunnel_endpoint,
            } => {
                writeln!(out, "    Creating account for IP: {client_ip}")?;
                spinner.inc(1);
                let user_pk = ledger.create_user(CreateUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip,
                    tunnel_endpoint,
                    tenant_pk: None,
                    ip_proof: ip_proof.get(out)?,
                })?;
                spinner.set_message("Multicast user created");
                user_pk
            }
        };

        self.poll_for_user_activated(ledger, &user_pk, spinner)?;
        spinner.set_message("Joining feed(s)...");
        ledger.subscribe_feed(SubscribeFeedCommand {
            user_pk,
            feed_pks: join.feed_pks,
        })?;
        Ok(())
    }

    fn parse_dz_mode(&self) -> eyre::Result<ParsedDzMode> {
        // `--tenant`/`--allocate-addr` belong to the bare form; the subcommand has
        // its own. Accepting them here would silently ignore what the caller typed.
        if self.tenant.is_some() || self.allocate_addr {
            eyre::bail!(
                "--tenant/--allocate-addr apply to the bare 'doublezero connect'; \
                 pass them to the subcommand instead, e.g. \
                 'doublezero connect ibrl <TENANT> --allocate-addr'"
            );
        }

        let Some(dz_mode) = &self.dz_mode else {
            // The bare form is routed to `execute_both` before this is reached.
            eyre::bail!("no connect mode given");
        };

        match dz_mode {
            DzMode::IBRL {
                tenant,
                allocate_addr,
            } => {
                if *allocate_addr {
                    Ok(ParsedDzMode::Ibrl(
                        UserType::IBRLWithAllocatedIP,
                        tenant.clone(),
                    ))
                } else {
                    Ok(ParsedDzMode::Ibrl(UserType::IBRL, tenant.clone()))
                }
            }
            DzMode::Multicast {
                mode,
                multicast_groups,
                pub_groups,
                sub_groups,
                sub_feeds,
                unsub_feeds,
            } => {
                let has_legacy = mode.is_some() || !multicast_groups.is_empty();
                let has_new = !pub_groups.is_empty() || !sub_groups.is_empty();
                let has_feeds = !sub_feeds.is_empty() || !unsub_feeds.is_empty();

                // clap's conflicts_with_all rejects these mixes at parse time; this covers a
                // programmatically built command.
                if has_feeds && (has_legacy || has_new) {
                    eyre::bail!("Cannot mix --subscribe-feed/--unsubscribe-feed with --publish/--subscribe or positional group arguments");
                }
                if has_feeds {
                    // Same-spelling overlap; a code overlapping the same feed's pubkey is caught
                    // after resolution in execute_multicast_feeds. Codes are case-sensitive.
                    if let Some(feed) = sub_feeds.iter().find(|f| unsub_feeds.contains(f)) {
                        eyre::bail!(
                            "feed {feed} is in both --subscribe-feed and --unsubscribe-feed"
                        );
                    }
                    return Ok(ParsedDzMode::MulticastFeeds {
                        sub_feeds: sub_feeds.clone(),
                        unsub_feeds: unsub_feeds.clone(),
                    });
                }

                if has_legacy && has_new {
                    eyre::bail!("Cannot mix legacy positional args (mode + groups) with --publish/--subscribe flags");
                }

                if has_legacy {
                    let mode = mode.as_ref().ok_or_else(|| {
                        eyre::eyre!("Multicast mode (publisher/subscriber) is required when using positional arguments")
                    })?;
                    if multicast_groups.is_empty() {
                        eyre::bail!("At least one multicast group code is required");
                    }
                    let (pg, sg) = match mode {
                        MulticastMode::Publisher => (multicast_groups.clone(), vec![]),
                        MulticastMode::Subscriber => (vec![], multicast_groups.clone()),
                    };
                    Ok(ParsedDzMode::Multicast {
                        pub_groups: pg,
                        sub_groups: sg,
                    })
                } else if has_new {
                    Ok(ParsedDzMode::Multicast {
                        pub_groups: pub_groups.clone(),
                        sub_groups: sub_groups.clone(),
                    })
                } else {
                    // No groups specified: auto-join every group authorized in the
                    // caller's AccessPass (resolved in execute_multicast).
                    Ok(ParsedDzMode::Multicast {
                        pub_groups: vec![],
                        sub_groups: vec![],
                    })
                }
            }
        }
    }

    async fn find_or_create_device<D: DaemonClient, L: LedgerClient>(
        &self,
        ledger: &L,
        daemon: &D,
        devices: &HashMap<Pubkey, Device>,
        spinner: &ProgressBar,
        exclude_ips: &[Ipv4Addr],
    ) -> eyre::Result<(Pubkey, Device, Ipv4Addr)> {
        spinner.set_message("Searching for the nearest device...");
        // filter out existing devices for users with existing tunnels
        // put some arbitrary cap on latency for second devices
        let (device_pk, tunnel_endpoint) = match self.device.as_ref() {
            Some(device) => {
                let pk = match device.parse::<Pubkey>() {
                    Ok(pubkey) => pubkey,
                    Err(_) => {
                        let (pubkey, _) = devices
                            .iter()
                            .find(|(_, d)| d.code == *device)
                            .ok_or(eyre::eyre!("Device not found"))?;
                        *pubkey
                    }
                };
                // For explicit device selection, use latency data to pick the best endpoint
                let latencies = retrieve_latencies(daemon, devices, false, Some(spinner)).await?;
                let dev = devices.get(&pk);
                let device_public_ip = dev.map(|d| d.public_ip).unwrap_or(Ipv4Addr::UNSPECIFIED);
                let endpoint = select_tunnel_endpoint(
                    &latencies,
                    &pk.to_string(),
                    device_public_ip,
                    exclude_ips,
                );
                (pk, endpoint)
            }
            None => {
                let latency =
                    best_latency(daemon, devices, true, Some(spinner), None, exclude_ips).await?;
                spinner.set_message("Reading device account...");
                let pk = Pubkey::from_str(&latency.device_pk)
                    .map_err(|_| eyre::eyre!("Unable to parse pubkey"))?;
                // Use select_tunnel_endpoint to pick the best available endpoint for this
                // device, respecting exclude_ips. best_latency picks the device but the
                // returned record's device_ip might be an excluded endpoint.
                let latencies = retrieve_latencies(daemon, devices, false, Some(spinner)).await?;
                let device_public_ip = devices
                    .get(&pk)
                    .map(|d| d.public_ip)
                    .unwrap_or(Ipv4Addr::UNSPECIFIED);
                let endpoint = select_tunnel_endpoint(
                    &latencies,
                    &pk.to_string(),
                    device_public_ip,
                    exclude_ips,
                );
                (pk, endpoint)
            }
        };

        // `select_tunnel_endpoint` reports "nothing left" as UNSPECIFIED, which is a legal
        // tunnel_endpoint onchain (`User::validate` only constrains it when non-zero) that the
        // controller reads as "use the device's public IP". Writing it would put this tunnel on
        // the same underlay source address as the tunnel that just took the last free endpoint —
        // exactly the GRE collision per-endpoint addressing exists to avoid. Auto-selection
        // already fails here inside `best_latency`; the explicit-`--device` path reaches this
        // line instead, because it skips `best_latency` entirely.
        if tunnel_endpoint == Ipv4Addr::UNSPECIFIED {
            return Err(NoAvailableEndpoint.into());
        }

        let device = ledger
            .get_device(device_pk.to_string())
            .map_err(|_| eyre::eyre!("Unable to get device"))?;

        // If user explicitly specified a device, check if it's eligible
        if self.device.is_some() && !device.is_device_eligible_for_provisioning() {
            return Err(eyre::eyre!(
                "Device is not accepting more users (at capacity or max_users=0)"
            ));
        }

        Ok((device_pk, device, tunnel_endpoint))
    }

    #[allow(clippy::too_many_arguments)]
    async fn find_or_create_user<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        client_ip: &Ipv4Addr,
        spinner: &ProgressBar,
        user_type: UserType,
        tenant: Option<String>,
        ip_proof: &IpProofFetcher<'_>,
        out: &mut W,
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = ledger.list_user()?;
        let mut devices = ledger.list_device()?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            devices.retain(|_, d| {
                d.is_device_eligible_for_provisioning()
                    && d.check_user_type_capacity(user_type, false).is_none()
            });
        }

        // Find user by both client_ip AND user_type to support multiple tunnel types per IP
        let matched_user = users
            .iter()
            .find(|(_, u)| u.client_ip == *client_ip && u.user_type == user_type);

        let user_pubkey = match matched_user {
            Some((pubkey, user)) => {
                writeln!(out, "    An account already exists with Pubkey: {pubkey}")?;
                if user.status == UserStatus::Banned {
                    writeln!(out, "❌  The user is banned.")?;
                    eyre::bail!("User is banned.");
                }

                *pubkey
            }
            None => {
                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(ledger, daemon, &devices, spinner, &exclude_ips)
                    .await?;

                writeln!(out, "    Creating account...")?;
                writeln!(out, "    Device selected: {}", device.code)?;
                spinner.inc(1);

                // Check per-type user limit before attempting to create
                if let Some(err_msg) = device.check_user_type_capacity(user_type, false) {
                    return Err(eyre::eyre!(err_msg));
                }

                let accesspass = ledger
                    .get_accesspass(*client_ip, ledger.get_payer())?
                    .ok_or_else(|| {
                        eyre::eyre!(
                            "No valid AccessPass found for IP: {} user_payer: {}",
                            client_ip,
                            ledger.get_payer()
                        )
                    })?;

                // Determine tenant: 1) from CLI argument, 2) from config file, 3) from access pass allowlist
                // TODO(RFC-20 §Module contract): the config-file read below is a
                // parity-preserving carryover from the binary; module crates
                // should read resolved values from `CliContext`. Move tenant
                // resolution into the binary/`CliContext` in a follow-up.
                let tenant_with_source: Option<(String, &str)> = if let Some(t) = tenant {
                    Some((t, "CLI argument"))
                } else {
                    let cfg_tenant = doublezero_sdk::read_doublezero_config()
                        .ok()
                        .and_then(|(_, cfg)| cfg.tenant);
                    if let Some(t) = cfg_tenant {
                        Some((t, "configuration file"))
                    } else {
                        accesspass
                            .tenant_allowlist
                            .first()
                            .filter(|pk| **pk != Pubkey::default())
                            .map(|pk| (pk.to_string(), "Access Pass"))
                    }
                };

                let tenant_pk = match tenant_with_source {
                    Some((tenant_str, source)) => {
                        let (pubkey, tenant_account) = ledger
                            .get_tenant(tenant_str.clone())
                            .map_err(|_| eyre::eyre!("Tenant '{}' not found", tenant_str))?;
                        writeln!(
                            out,
                            "    Using tenant '{}' from {}.",
                            tenant_account.code, source
                        )?;
                        Some(pubkey)
                    }
                    None => None,
                };

                let res = ledger.create_user(CreateUserCommand {
                    user_type,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    tunnel_endpoint,
                    tenant_pk,
                    ip_proof: ip_proof.get(out)?,
                });

                match res {
                    Ok(pubkey) => {
                        spinner.set_message("User created");
                        pubkey
                    }
                    Err(e) => {
                        writeln!(out, "❌ Error creating user")?;
                        writeln!(out, "\nError: {e:?}\n")?;

                        return Err(eyre::eyre!("Error creating user: {e:?}"));
                    }
                }
            }
        };

        let user = self.poll_for_user_activated(ledger, &user_pubkey, spinner)?;

        Ok((user_pubkey, user))
    }

    #[allow(clippy::too_many_arguments)]
    async fn find_or_create_user_and_subscribe<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        client_ip: &Ipv4Addr,
        spinner: &ProgressBar,
        pub_group_pks: &[Pubkey],
        sub_group_pks: &[Pubkey],
        ip_proof: &IpProofFetcher<'_>,
        out: &mut W,
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = ledger.list_user()?;
        let mut devices = ledger.list_device()?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            let is_publisher = !pub_group_pks.is_empty();
            devices.retain(|_, d| {
                d.is_device_eligible_for_provisioning()
                    && d.check_user_type_capacity(UserType::Multicast, is_publisher)
                        .is_none()
            });
        }

        // Find all users for this IP - multiple user accounts per IP are allowed (one per UserType)
        let matched_users: Vec<_> = users
            .iter()
            .filter(|(_, u)| u.client_ip == *client_ip)
            .collect();

        let ibrl_user = matched_users
            .iter()
            .find(|(_, u)| matches!(u.user_type, UserType::IBRL | UserType::IBRLWithAllocatedIP))
            .copied();

        let mcast_user = matched_users
            .iter()
            .find(|(_, u)| u.user_type == UserType::Multicast)
            .copied();

        // Combine all group pks (deduplicated) for user creation (first group goes in create_subscribe_user)
        let mut all_group_pks: Vec<Pubkey> = Vec::new();
        for pk in pub_group_pks.iter().chain(sub_group_pks.iter()) {
            if !all_group_pks.contains(pk) {
                all_group_pks.push(*pk);
            }
        }

        let user_pubkey = match (ibrl_user, mcast_user) {
            // IBRL user exists but no Multicast user - create a separate Multicast user
            // This allows concurrent unicast (IBRL) and multicast tunnels for the same client IP
            (Some((ibrl_user_pk, ibrl_user)), None) => {
                // Select a separate device from the IBRL user to allow independent tunnels
                // Exclude the IBRL user's tunnel endpoint to ensure we get a different device
                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(ledger, daemon, &devices, spinner, &exclude_ips)
                    .await?;

                writeln!(
                    out,
                    "    Creating separate Multicast user for concurrent tunnels (IBRL user: {})",
                    ibrl_user_pk
                )?;
                writeln!(out, "    Device selected: {}", device.code)?;

                // Check per-type user limit before attempting to create
                if let Some(err_msg) =
                    device.check_user_type_capacity(UserType::Multicast, !pub_group_pks.is_empty())
                {
                    return Err(eyre::eyre!(err_msg));
                }

                // Create the user subscribed to every group sharing the first group's
                // flag pair in one transaction; other flag pairs follow as batched
                // role updates.
                let (create_group_pks, (create_publisher, create_subscriber), follow_up_batches) =
                    plan_group_batches(&all_group_pks, pub_group_pks, sub_group_pks);
                if create_group_pks.is_empty() {
                    eyre::bail!("At least one multicast group is required");
                }

                let res = ledger.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: ibrl_user.cyoa_type,
                    client_ip: *client_ip,
                    publisher: create_publisher,
                    subscriber: create_subscriber,
                    mgroup_pks: create_group_pks,
                    tunnel_endpoint,
                    owner: None,
                    feed_pk: None,
                    ip_proof: ip_proof.get(out)?,
                });

                let user_pk = match res {
                    Ok(user_pk) => {
                        spinner.set_message("Multicast user created");
                        user_pk
                    }
                    Err(e) => {
                        writeln!(out, "❌ Error creating Multicast user")?;
                        writeln!(out, "\nError: {e:?}\n")?;
                        eyre::bail!("Error creating Multicast user: {:?}", e);
                    }
                };

                // Groups with other flag pairs need the user Activated before their
                // role updates; the common case (all groups share one flag pair)
                // skips the wait entirely.
                if !follow_up_batches.is_empty() {
                    self.poll_for_user_activated(ledger, &user_pk, spinner)?;
                }
                for (publisher, subscriber, group_pks) in follow_up_batches {
                    spinner
                        .set_message(format!("Subscribing to {} more group(s)", group_pks.len()));
                    ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                        user_pk,
                        group_pks,
                        client_ip: *client_ip,
                        publisher,
                        subscriber,
                        device_pk: None,
                        feed_pk: None,
                    })?;
                }

                user_pk
            }
            // Both IBRL and Multicast users exist - add subscription to existing Multicast user
            (Some(_), Some((user_pk, user))) | (None, Some((user_pk, user))) => {
                // Ensure user is activated before subscribing to new groups
                if user.status != UserStatus::Activated {
                    self.poll_for_user_activated(ledger, user_pk, spinner)?;
                }

                // Add the requested roles, batched by each group's effective
                // (publisher, subscriber) flag pair. The instruction sets absolute
                // role state, so the desired flags union the request with the roles
                // the user already holds — a group in both --publish and --subscribe
                // (or already holding the other role) keeps both instead of the last
                // write stripping the first.
                let mut batches: Vec<(bool, bool, Vec<Pubkey>)> = Vec::new();
                for group_pk in all_group_pks.iter() {
                    let publisher =
                        pub_group_pks.contains(group_pk) || user.publishers.contains(group_pk);
                    let subscriber =
                        sub_group_pks.contains(group_pk) || user.subscribers.contains(group_pk);
                    // Skip groups whose desired state is already onchain.
                    if user.publishers.contains(group_pk) == publisher
                        && user.subscribers.contains(group_pk) == subscriber
                    {
                        continue;
                    }
                    match batches.iter_mut().find(|(p, s, pks)| {
                        (*p, *s) == (publisher, subscriber)
                            && pks.len() < MAX_GROUPS_PER_TRANSACTION
                    }) {
                        Some((_, _, pks)) => pks.push(*group_pk),
                        None => batches.push((publisher, subscriber, vec![*group_pk])),
                    }
                }
                for (publisher, subscriber, group_pks) in batches {
                    spinner.set_message(format!(
                        "Adding subscription to existing Multicast user: {user_pk}"
                    ));

                    let res =
                        ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                            user_pk: *user_pk,
                            group_pks,
                            client_ip: *client_ip,
                            publisher,
                            subscriber,
                            device_pk: None,
                            feed_pk: None,
                        });

                    match res {
                        Ok(_) => {
                            spinner.set_message("Subscription added");
                        }
                        Err(e) => {
                            writeln!(out, "❌ Error adding subscription")?;
                            writeln!(out, "\nError: {e:?}\n")?;
                            eyre::bail!("Error adding subscription to existing user: {e:?}");
                        }
                    }
                }

                *user_pk
            }
            // No user exists, create a new Multicast user
            (None, None) => {
                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(ledger, daemon, &devices, spinner, &exclude_ips)
                    .await?;

                writeln!(out, "    Creating account for IP: {client_ip}")?;
                writeln!(out, "    Device selected: {}", device.code)?;
                spinner.inc(1);

                // Check per-type user limit before attempting to create
                if let Some(err_msg) =
                    device.check_user_type_capacity(UserType::Multicast, !pub_group_pks.is_empty())
                {
                    return Err(eyre::eyre!(err_msg));
                }

                // Create the user subscribed to every group sharing the first group's
                // flag pair in one transaction; other flag pairs follow as batched
                // role updates.
                let (create_group_pks, (create_publisher, create_subscriber), follow_up_batches) =
                    plan_group_batches(&all_group_pks, pub_group_pks, sub_group_pks);
                if create_group_pks.is_empty() {
                    eyre::bail!("At least one multicast group is required");
                }

                let res = ledger.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    publisher: create_publisher,
                    subscriber: create_subscriber,
                    mgroup_pks: create_group_pks,
                    tunnel_endpoint,
                    owner: None,
                    feed_pk: None,
                    ip_proof: ip_proof.get(out)?,
                });

                let user_pk = match res {
                    Ok(pubkey) => {
                        spinner.set_message("User created");
                        pubkey
                    }
                    Err(e) => {
                        writeln!(out, "❌ Error creating user")?;
                        writeln!(out, "\nError: {e:?}\n")?;
                        return Err(eyre::eyre!("Error creating user: {e:?}"));
                    }
                };

                // Groups with other flag pairs need the user Activated before their
                // role updates; the common case (all groups share one flag pair)
                // skips the wait entirely.
                if !follow_up_batches.is_empty() {
                    self.poll_for_user_activated(ledger, &user_pk, spinner)?;
                }
                for (publisher, subscriber, group_pks) in follow_up_batches {
                    spinner
                        .set_message(format!("Subscribing to {} more group(s)", group_pks.len()));
                    ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                        user_pk,
                        group_pks,
                        client_ip: *client_ip,
                        publisher,
                        subscriber,
                        device_pk: None,
                        feed_pk: None,
                    })?;
                }

                user_pk
            }
        };

        let user = self.poll_for_user_activated(ledger, &user_pubkey, spinner)?;

        Ok((user_pubkey, user))
    }

    fn poll_for_user_activated<L: LedgerClient>(
        &self,
        ledger: &L,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<User> {
        spinner.set_message("Reading user account...");

        // User accounts are created atomically in Activated status, but the RPC
        // node we read from may lag a few seconds behind the slot the create
        // transaction landed in — retry until the account is visible.
        let builder = ExponentialBuilder::new()
            .with_max_times(6)
            .with_min_delay(Duration::from_secs(1))
            .with_max_delay(Duration::from_secs(8));

        let get_user = || ledger.get_user(*user_pubkey);

        get_user
            .retry(builder)
            .notify(|_, dur| {
                spinner.set_message(format!("Reading user account (retrying in {dur:?})..."))
            })
            .call()
            .map_err(|_| eyre::eyre!("Timeout reading user account"))
    }

    async fn user_activated<D: DaemonClient, W: Write>(
        &self,
        daemon: &D,
        user_type: UserType,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        spinner.inc(1);

        // Enable the reconciler (no-op if already enabled).
        if let Err(e) = daemon.enable().await {
            // Check if the reconciler is already enabled despite the enable call failing.
            let already_enabled = daemon
                .v2_status()
                .await
                .map(|s| s.reconciler_enabled)
                .unwrap_or(false);
            if !already_enabled {
                writeln!(
                    out,
                    "    Error: failed to enable reconciler: {e}. Tunnel will not be provisioned."
                )?;
                return Ok(());
            }
        }

        spinner.set_message("User activated, waiting for daemon to provision tunnel...");

        let user_type_str = user_type.to_string();
        match self
            .poll_for_daemon_provisioned(daemon, &user_type_str, spinner)
            .await
        {
            Ok(status) => {
                spinner.inc(1);
                if let Some(src) = &status.tunnel_src {
                    writeln!(out, "    Tunnel Src: {src}")?;
                }
                if let Some(dst) = &status.tunnel_dst {
                    writeln!(out, "    Tunnel Dst: {dst}")?;
                }
                if let Some(ip) = &status.doublezero_ip {
                    writeln!(out, "    DoubleZero IP: {ip}")?;
                }
                writeln!(
                    out,
                    "    Session: {}",
                    status.doublezero_status.session_status
                )?;
            }
            Err(e) => {
                spinner.inc(1);
                writeln!(
                    out,
                    "    Tunnel provisioning in progress (daemon will handle it): {e}"
                )?;
            }
        }

        Ok(())
    }

    async fn poll_for_daemon_provisioned<D: DaemonClient>(
        &self,
        daemon: &D,
        user_type_str: &str,
        spinner: &ProgressBar,
    ) -> eyre::Result<StatusResponse> {
        // Poll for up to ~60s (reconciler polls every 10s by default)
        let max_attempts = 12;
        let delay = Duration::from_secs(5);

        for attempt in 0..max_attempts {
            if attempt > 0 {
                spinner.set_message("waiting for tunnel provisioning...");
                tokio::time::sleep(delay).await;
            }

            if let Ok(statuses) = daemon.status().await {
                if let Some(status) = statuses
                    .iter()
                    .find(|s| s.user_type.as_ref().is_some_and(|ut| ut == user_type_str))
                {
                    return Ok(status.clone());
                }
            }
        }

        eyre::bail!("timed out waiting for daemon to provision tunnel")
    }
}

/// Upper bound on multicast groups folded into a single CreateSubscribeUser
/// transaction. The instruction already carries the device's dz_prefix blocks, so
/// its account headroom under the 1232-byte transaction size limit is smaller than
/// UpdateMulticastGroupRoles'; 8 groups leave room for several dz_prefix blocks
/// (devices typically advertise one or two). Overflow rides in follow-up update
/// batches, which are chunked to [`MAX_GROUPS_PER_TRANSACTION`].
const MAX_CREATE_GROUPS: usize = 8;

/// One follow-up UpdateMulticastGroupRoles batch: (publisher, subscriber, group_pks).
type RoleBatch = (bool, bool, Vec<Pubkey>);

/// Split the deduplicated group list into the batch folded into the
/// CreateSubscribeUser transaction — every group sharing the first group's
/// (publisher, subscriber) flag pair, up to [`MAX_CREATE_GROUPS`] — and the
/// follow-up UpdateMulticastGroupRoles batches for the rest, grouped by flag pair
/// and chunked to [`MAX_GROUPS_PER_TRANSACTION`]. Returns the create batch, its
/// shared flag pair, and the follow-up batches. In the common case (all groups
/// share one flag pair) the follow-up list is empty and connect is a single
/// transaction.
fn plan_group_batches(
    all_group_pks: &[Pubkey],
    pub_group_pks: &[Pubkey],
    sub_group_pks: &[Pubkey],
) -> (Vec<Pubkey>, (bool, bool), Vec<RoleBatch>) {
    let flags_of = |pk: &Pubkey| (pub_group_pks.contains(pk), sub_group_pks.contains(pk));
    let Some(first_flags) = all_group_pks.first().map(flags_of) else {
        return (Vec::new(), (false, false), Vec::new());
    };

    let mut create_group_pks = Vec::new();
    let mut follow_ups: Vec<RoleBatch> = Vec::new();
    for pk in all_group_pks {
        let (publisher, subscriber) = flags_of(pk);
        if (publisher, subscriber) == first_flags && create_group_pks.len() < MAX_CREATE_GROUPS {
            create_group_pks.push(*pk);
            continue;
        }
        // A full batch stops matching, so oversize flag pairs chunk naturally.
        match follow_ups.iter_mut().find(|(p, s, pks)| {
            (*p, *s) == (publisher, subscriber) && pks.len() < MAX_GROUPS_PER_TRANSACTION
        }) {
            Some((_, _, pks)) => pks.push(*pk),
            None => follow_ups.push((publisher, subscriber, vec![*pk])),
        }
    }
    (create_group_pks, first_flags, follow_ups)
}

fn exclude_ips(
    users: &HashMap<Pubkey, User>,
    client_ip: &Ipv4Addr,
    devices: &HashMap<Pubkey, Device>,
) -> Vec<Ipv4Addr> {
    users
        .iter()
        .filter(|(_, u)| u.client_ip == *client_ip && u.has_unicast_tunnel())
        .map(|(_, u)| {
            if u.has_tunnel_endpoint() {
                u.tunnel_endpoint
            } else {
                devices
                    .get(&u.device_pk)
                    .map(|d| d.public_ip)
                    .unwrap_or(Ipv4Addr::UNSPECIFIED)
            }
        })
        .filter(|ip| *ip != Ipv4Addr::UNSPECIFIED)
        .collect()
}

/// What auto-selection decided, so the caller can tell the operator about every feed it did not
/// take and why.
struct FeedSelection {
    join: Vec<Pubkey>,
    full: Vec<Pubkey>,
    other_metro: Vec<Pubkey>,
    over_feed_limit: Vec<Pubkey>,
    held: Vec<Pubkey>,
    unknown: Vec<Pubkey>,
}

/// A feed's code, or its pubkey if the feed account cannot be found. Shared by every place that
/// needs a feed's display name — operator output and the selection sort order alike — so the
/// lookup-and-fallback logic exists in exactly one place.
fn feed_code_or_pubkey(feed_pk: &Pubkey, feeds: &HashMap<Pubkey, Feed>) -> String {
    feeds
        .get(feed_pk)
        .map(|feed| feed.code.clone())
        .unwrap_or_else(|| feed_pk.to_string())
}

/// A human-readable metro name for `exchange_pk`: the exchange's `name`, falling back to its
/// `code` when the name is empty, or `None` when the exchange cannot be resolved at all. Every
/// caller is cosmetic — a device code conventionally leads with the metro, but that is a naming
/// convention, not data, and it does not read as a place to a new customer — so a resolve
/// failure must degrade to today's wording, never fail the connect.
fn metro_name(exchange_pk: &Pubkey, exchanges: &HashMap<Pubkey, Exchange>) -> Option<String> {
    exchanges.get(exchange_pk).map(|exchange| {
        if exchange.name.is_empty() {
            exchange.code.clone()
        } else {
            exchange.name.clone()
        }
    })
}

/// "{feed code} is served from {metro name}", or just the feed code when either the feed or its
/// exchange cannot be resolved. See `metro_name` for why a resolve failure degrades quietly.
fn feed_metro_description(
    feed_pk: &Pubkey,
    feeds: &HashMap<Pubkey, Feed>,
    exchanges: &HashMap<Pubkey, Exchange>,
) -> String {
    let code = feed_code_or_pubkey(feed_pk, feeds);
    match feeds
        .get(feed_pk)
        .and_then(|feed| metro_name(&feed.exchange, exchanges))
    {
        Some(metro) => format!("{code} is served from {metro}"),
        None => code,
    }
}

/// A closing clause naming the metro(s) resolved for `exchange_pks` — "Amsterdam", or "Amsterdam
/// or Frankfurt" for several — falling back to the generic "that metro" when none resolve. Shared
/// by both failure messages that point the operator toward a metro.
fn destination_clause(exchange_pks: &[Pubkey], exchanges: &HashMap<Pubkey, Exchange>) -> String {
    let mut names: Vec<String> = Vec::new();
    for exchange_pk in exchange_pks {
        if let Some(name) = metro_name(exchange_pk, exchanges) {
            if !names.contains(&name) {
                names.push(name);
            }
        }
    }
    match names.len() {
        0 => "that metro".to_string(),
        1 => names.remove(0),
        _ => names.join(" or "),
    }
}

/// Feed seats ordered by their feed's code (or its pubkey if unresolved) — the ordering
/// `select_purchased_feeds` and the pre-device capacity check share, so a feed's position in an
/// error message never depends on account iteration order.
fn feed_seats_by_code<'a>(
    seats: &'a [FeedSeat],
    feeds: &HashMap<Pubkey, Feed>,
) -> Vec<&'a FeedSeat> {
    let mut ordered: Vec<&FeedSeat> = seats.iter().collect();
    ordered.sort_by(|left, right| {
        feed_code_or_pubkey(&left.feed_key, feeds).cmp(&feed_code_or_pubkey(&right.feed_key, feeds))
    });
    ordered
}

// Latency tolerance for the informational "a nearer device exists outside your feeds' metro"
// notice: jitter below 1ms is noise, not a genuine gap worth mentioning.
const LOWER_LATENCY_NOTICE_THRESHOLD_NS: i64 = 1_000_000;

/// Render nanoseconds the way `client::display_as_ms` renders a `LatencyRecord` field, without
/// depending on that private helper from another module.
fn format_latency_ms(latency_ns: i64) -> String {
    format!("{:.2}ms", latency_ns as f64 / 1_000_000.0)
}

/// The same ranking device selection uses (`latency::compare_latency_min_then_avg`: min latency,
/// then avg), replicated here since that comparator is private to the `latency` module. Used so
/// the lower-latency notice names the same device selection itself would have preferred.
fn compare_latency_records(a: &LatencyRecord, b: &LatencyRecord) -> std::cmp::Ordering {
    a.min_latency_ns
        .cmp(&b.min_latency_ns)
        .then_with(|| a.avg_latency_ns.cmp(&b.avg_latency_ns))
}

/// Choose which of the pass's purchased feeds this machine should join, given the device it will
/// connect through and the feeds it already holds.
///
/// Feeds are considered in code order so that which ones get taken when the feed limit binds is
/// predictable rather than dependent on account iteration order. Headroom is measured against
/// `max_users`, the field the program's `try_add_feed_user` actually enforces — `max_future_users`
/// is unread today, so trusting it would propose feeds the program then rejects.
///
/// `MAX_USER_FEEDS` is the only bound applied. A join too large for one transaction is split by
/// `SubscribeFeedCommand`, so selecting group-heavy feeds here is safe.
fn select_purchased_feeds(
    seats: &[FeedSeat],
    feeds: &HashMap<Pubkey, Feed>,
    device_exchange: &Pubkey,
    held_feed_pks: &[Pubkey],
) -> FeedSelection {
    let mut selection = FeedSelection {
        join: Vec::new(),
        full: Vec::new(),
        other_metro: Vec::new(),
        over_feed_limit: Vec::new(),
        held: Vec::new(),
        unknown: Vec::new(),
    };

    let ordered = feed_seats_by_code(seats, feeds);

    // Computed once, before the loop: `held_feed_pks` is fixed for the whole call, so counting it
    // here (rather than growing `selection.held` as the code-ordered loop happens to reach each
    // held feed) makes the cap check order-independent. It also covers feeds the user holds that
    // are not seats on this pass at all — the program's own MAX_USER_FEEDS check counts every held
    // feed, not just the ones this pass currently lists.
    let held_count = held_feed_pks.len();

    for seat in ordered {
        if held_feed_pks.contains(&seat.feed_key) {
            selection.held.push(seat.feed_key);
            continue;
        }
        let Some(feed) = feeds.get(&seat.feed_key) else {
            selection.unknown.push(seat.feed_key);
            continue;
        };
        if feed.exchange != *device_exchange {
            selection.other_metro.push(seat.feed_key);
            continue;
        }
        if seat.current_users >= seat.max_users {
            selection.full.push(seat.feed_key);
            continue;
        }
        if held_count + selection.join.len() >= MAX_USER_FEEDS {
            selection.over_feed_limit.push(seat.feed_key);
            continue;
        }
        selection.join.push(seat.feed_key);
    }

    selection
}

/// Resolve `--subscribe-feed` values.
fn resolve_feeds_for_metro(
    names: &[String],
    feeds: &HashMap<Pubkey, Feed>,
    device: &Device,
    wrong_metro_hint: &str,
) -> eyre::Result<Vec<Pubkey>> {
    let mut resolved: Vec<Pubkey> = Vec::with_capacity(names.len());
    for name in names {
        let pk = match name.parse::<Pubkey>() {
            Ok(pk) => pk,
            Err(_) => {
                let in_metro = feeds
                    .iter()
                    .find(|(_, f)| f.exchange == device.exchange_pk && f.code == *name);
                match in_metro {
                    Some((pk, _)) => *pk,
                    None => {
                        if feeds.values().any(|f| f.code == *name) {
                            eyre::bail!(
                                "feed {name} does not serve the metro of device {}; {wrong_metro_hint}",
                                device.code
                            );
                        }
                        eyre::bail!("feed {name} not found");
                    }
                }
            }
        };
        if resolved.contains(&pk) {
            eyre::bail!("duplicate feed: {name}");
        }
        resolved.push(pk);
    }
    Ok(resolved)
}

/// Resolve `--unsubscribe-feed` values against the feeds the user holds. A pubkey passes through
/// (the SDK command checks it is held); a code must name a held feed.
fn resolve_held_feeds(
    names: &[String],
    feeds: &HashMap<Pubkey, Feed>,
    user: &User,
) -> eyre::Result<Vec<Pubkey>> {
    let mut resolved: Vec<Pubkey> = Vec::with_capacity(names.len());
    for name in names {
        let pk = match name.parse::<Pubkey>() {
            Ok(pk) => pk,
            Err(_) => {
                let held = feeds
                    .iter()
                    .find(|(pk, f)| user.feed_pks.contains(pk) && f.code == *name);
                match held {
                    Some((pk, _)) => *pk,
                    None => {
                        if feeds.values().any(|f| f.code == *name) {
                            eyre::bail!("you do not hold feed {name}");
                        }
                        eyre::bail!("feed {name} not found");
                    }
                }
            }
        };
        if resolved.contains(&pk) {
            eyre::bail!("duplicate feed: {name}");
        }
        resolved.push(pk);
    }
    Ok(resolved)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{
            DoubleZeroStatus, LatencyRecord, LatencyResponse, MockDaemonClient, StatusResponse,
            V2StatusResponse,
        },
        ip_proof::MockIpProofClient,
        ledger::MockLedgerClient,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_config::Environment;
    use doublezero_sdk::{tests::utils::create_temp_config, utils::parse_pubkey};
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
        accounttype::AccountType,
        device::{Device, DeviceStatus, DeviceType},
        exchange::{Exchange, ExchangeStatus},
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
    };
    use mockall::predicate;
    use std::{
        collections::{HashMap, HashSet},
        sync::{Arc, Mutex, OnceLock},
        thread,
        time::Duration,
    };
    use tempfile::TempDir;

    static TMPDIR: OnceLock<TempDir> = OnceLock::new();

    fn get_temp_dir() -> &'static TempDir {
        TMPDIR.get_or_init(|| create_temp_config().expect("Failed to create temp config"))
    }

    // Point DOUBLEZERO_CONFIG_FILE at a fresh temp config (with no tenant set)
    // before any test runs: `connect` reads the config file during tenant
    // resolution, and a developer's real config must not leak into tests.
    #[ctor::ctor(unsafe)]
    fn setup() {
        let temp_dir = get_temp_dir();
        println!("Using TMPDIR = {}", temp_dir.path().display());
    }

    /// Build a seat for `feed_key` with an explicit cap and live count.
    fn seat(feed_key: Pubkey, max_users: u8, current_users: u8) -> FeedSeat {
        FeedSeat {
            feed_key,
            max_users,
            current_users,
            ..Default::default()
        }
    }

    /// Build a one-group feed in `exchange` with code `code`.
    fn feed_in(code: &str, exchange: Pubkey) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: code.to_string(),
            name: code.to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        }
    }

    /// Both feeds have headroom in the device's metro, so both are selected, ordered by code.
    #[test]
    fn test_select_purchased_feeds_takes_every_feed_with_headroom() {
        let exchange = Pubkey::new_unique();
        let (kalshi_pk, shreds_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut feeds = HashMap::new();
        feeds.insert(kalshi_pk, feed_in("kalshi", exchange));
        feeds.insert(shreds_pk, feed_in("shreds", exchange));
        let seats = [seat(shreds_pk, 2, 0), seat(kalshi_pk, 1, 0)];

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &[]);

        assert_eq!(selection.join, vec![kalshi_pk, shreds_pk]);
        assert!(selection.full.is_empty());
        assert!(selection.other_metro.is_empty());
    }

    /// A full seat is skipped, not an error at this layer: the caller decides.
    #[test]
    fn test_select_purchased_feeds_skips_a_full_seat() {
        let exchange = Pubkey::new_unique();
        let (kalshi_pk, shreds_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut feeds = HashMap::new();
        feeds.insert(kalshi_pk, feed_in("kalshi", exchange));
        feeds.insert(shreds_pk, feed_in("shreds", exchange));
        let seats = [seat(shreds_pk, 2, 1), seat(kalshi_pk, 1, 1)];

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &[]);

        assert_eq!(selection.join, vec![shreds_pk]);
        assert_eq!(selection.full, vec![kalshi_pk]);
    }

    /// A feed already held is reported as held and never re-joined, so a re-run is a no-op.
    #[test]
    fn test_select_purchased_feeds_reports_held_feeds_separately() {
        let exchange = Pubkey::new_unique();
        let shreds_pk = Pubkey::new_unique();
        let mut feeds = HashMap::new();
        feeds.insert(shreds_pk, feed_in("shreds", exchange));
        // The seat is full precisely because this machine holds it.
        let seats = [seat(shreds_pk, 1, 1)];

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &[shreds_pk]);

        assert!(selection.join.is_empty());
        assert_eq!(selection.held, vec![shreds_pk]);
        assert!(
            selection.full.is_empty(),
            "a feed this machine holds must not also be reported full"
        );
    }

    /// A feed serving another exchange lands in other_metro, never in join.
    #[test]
    fn test_select_purchased_feeds_separates_other_metro() {
        let here = Pubkey::new_unique();
        let away = Pubkey::new_unique();
        let (near_pk, far_pk) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut feeds = HashMap::new();
        feeds.insert(near_pk, feed_in("shreds", here));
        feeds.insert(far_pk, feed_in("kalshi", away));
        let seats = [seat(near_pk, 1, 0), seat(far_pk, 1, 0)];

        let selection = select_purchased_feeds(&seats, &feeds, &here, &[]);

        assert_eq!(selection.join, vec![near_pk]);
        assert_eq!(selection.other_metro, vec![far_pk]);
    }

    /// A seat naming a feed account that cannot be read is reported, not silently dropped.
    #[test]
    fn test_select_purchased_feeds_reports_unknown_feed_accounts() {
        let exchange = Pubkey::new_unique();
        let missing_pk = Pubkey::new_unique();
        let seats = [seat(missing_pk, 1, 0)];

        let selection = select_purchased_feeds(&seats, &HashMap::new(), &exchange, &[]);

        assert!(selection.join.is_empty());
        assert_eq!(selection.unknown, vec![missing_pk]);
    }

    /// The per-user feed cap bounds the join. Nothing here bounds it by transaction size: the SDK
    /// splits an oversized join across transactions on its own.
    #[test]
    fn test_select_purchased_feeds_respects_the_per_user_feed_cap() {
        let exchange = Pubkey::new_unique();
        let mut feeds = HashMap::new();
        let mut seats = Vec::new();
        for index in 0..(MAX_USER_FEEDS + 2) {
            let feed_pk = Pubkey::new_unique();
            // Two-digit codes keep the lexicographic order equal to the numeric order.
            feeds.insert(feed_pk, feed_in(&format!("feed{index:02}"), exchange));
            seats.push(seat(feed_pk, 1, 0));
        }

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &[]);

        assert_eq!(selection.join.len(), MAX_USER_FEEDS);
        assert_eq!(selection.over_feed_limit.len(), 2);
    }

    /// Feeds heavy enough to need several transactions are still all selected: splitting them is
    /// the SDK's job, so selection must not silently drop any.
    #[test]
    fn test_select_purchased_feeds_does_not_bound_by_transaction_size() {
        let exchange = Pubkey::new_unique();
        let mut feeds = HashMap::new();
        let mut seats = Vec::new();
        // 3 feeds x 14 groups = 42 group accounts, well past one transaction's 25.
        for index in 0..3 {
            let feed_pk = Pubkey::new_unique();
            let mut feed = feed_in(&format!("feed{index:02}"), exchange);
            feed.groups = (0..14).map(|_| Pubkey::new_unique()).collect();
            feeds.insert(feed_pk, feed);
            seats.push(seat(feed_pk, 1, 0));
        }

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &[]);

        assert_eq!(selection.join.len(), 3);
        assert!(selection.over_feed_limit.is_empty());
    }

    /// Regression: the feed cap must count every held feed, not just the ones the code-ordered
    /// loop has reached so far. These held feeds ("zzz...") sort after every candidate
    /// ("aaa...") by code, so counting `selection.held.len()` — built during the same loop —
    /// would still be zero at the point each candidate is considered, letting all of them
    /// through and proposing a join the program's own MAX_USER_FEEDS check would reject.
    #[test]
    fn test_select_purchased_feeds_caps_against_held_feeds_sorted_after_candidates() {
        let exchange = Pubkey::new_unique();
        let mut feeds = HashMap::new();
        let mut seats = Vec::new();

        let held_count = 3;
        let held_feed_pks: Vec<Pubkey> = (0..held_count)
            .map(|index| {
                let feed_pk = Pubkey::new_unique();
                feeds.insert(feed_pk, feed_in(&format!("zzz-held-{index}"), exchange));
                seats.push(seat(feed_pk, 1, 0));
                feed_pk
            })
            .collect();
        for index in 0..MAX_USER_FEEDS {
            let feed_pk = Pubkey::new_unique();
            feeds.insert(
                feed_pk,
                feed_in(&format!("aaa-candidate-{index:02}"), exchange),
            );
            seats.push(seat(feed_pk, 1, 0));
        }

        let selection = select_purchased_feeds(&seats, &feeds, &exchange, &held_feed_pks);

        assert_eq!(selection.held.len(), held_count);
        assert_eq!(selection.join.len(), MAX_USER_FEEDS - held_count);
        assert_eq!(selection.over_feed_limit.len(), held_count);
    }

    struct TestFixture {
        pub ledger: MockLedgerClient,
        pub daemon: MockDaemonClient,
        pub devices: Arc<Mutex<HashMap<Pubkey, Device>>>,
        pub exchanges: Arc<Mutex<HashMap<Pubkey, Exchange>>>,
        pub users: Arc<Mutex<HashMap<Pubkey, User>>>,
        pub latencies: Arc<Mutex<Vec<LatencyRecord>>>,
        pub mcast_groups: Arc<Mutex<HashMap<Pubkey, MulticastGroup>>>,
        pub feeds: Arc<Mutex<HashMap<Pubkey, Feed>>>,
        pub tenants: Arc<Mutex<HashMap<Pubkey, Tenant>>>,
        pub default_tenant_pk: Pubkey,
        pub accesspass: Arc<Mutex<AccessPass>>,
        /// Tracks which service types the daemon has "provisioned" (simulating
        /// what the reconciler would do). The status mock only returns entries
        /// for types in this set.
        pub provisioned_services: Arc<Mutex<HashSet<String>>>,
    }

    impl TestFixture {
        pub fn new() -> Self {
            let mut fixture = Self::new_base();
            fixture.setup_enable(|| Ok(()));
            fixture.daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });
            fixture
        }

        pub fn new_with_failing_enable() -> Self {
            let mut fixture = Self::new_base();
            fixture.setup_enable(|| Err(eyre::eyre!("enable failed")));
            // When enable fails, the connect flow checks v2_status to see if the
            // reconciler is already enabled. Return disabled to simulate a genuine
            // enable failure. The first call also provides client_ip for the
            // connect flow's IP lookup.
            fixture.daemon.expect_v2_status().returning(|| {
                Ok(V2StatusResponse {
                    reconciler_enabled: false,
                    client_ip: "1.2.3.4".to_string(),
                    network: String::new(),
                    services: vec![],
                })
            });
            fixture
        }

        fn new_base() -> Self {
            // Create a default tenant
            let default_tenant_pk = Pubkey::new_unique();
            let default_tenant = Tenant {
                account_type: AccountType::Tenant,
                owner: Pubkey::new_unique(),
                bump_seed: 1,
                code: "test-tenant".to_string(),
                vrf_id: 100,
                reference_count: 0,
                administrators: vec![],
                payment_status: TenantPaymentStatus::Paid,
                token_account: Pubkey::default(),
                metro_routing: false,
                route_liveness: false,
                billing: TenantBillingConfig::default(),
                include_topologies: vec![],
            };

            let mut tenants = HashMap::new();
            tenants.insert(default_tenant_pk, default_tenant);

            let payer = Pubkey::new_unique();
            let accesspass = Arc::new(Mutex::new(AccessPass {
                account_type: AccountType::AccessPass,
                owner: payer,
                bump_seed: 1,
                client_ip: Ipv4Addr::new(1, 2, 3, 4),
                user_payer: payer,
                last_access_epoch: u64::MAX,
                accesspass_type: AccessPassType::Prepaid,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![],
                flags: 0,
                unicast_user_count: 0,
                max_unicast_users: 1,
                multicast_user_count: 0,
                max_multicast_users: 1,
            }));

            let mut fixture = Self {
                ledger: MockLedgerClient::new(),
                daemon: MockDaemonClient::new(),
                devices: Arc::new(Mutex::new(HashMap::new())),
                exchanges: Arc::new(Mutex::new(HashMap::new())),
                users: Arc::new(Mutex::new(HashMap::new())),
                latencies: Arc::new(Mutex::new(vec![])),
                mcast_groups: Arc::new(Mutex::new(HashMap::new())),
                feeds: Arc::new(Mutex::new(HashMap::new())),
                tenants: Arc::new(Mutex::new(tenants)),
                default_tenant_pk,
                accesspass,
                provisioned_services: Arc::new(Mutex::new(HashSet::new())),
            };

            fixture.ledger.expect_get_payer().return_const(payer);
            fixture.ledger.expect_get_epoch().returning(|| Ok(10));
            fixture
                .ledger
                .expect_check_requirements()
                .returning(|| Ok(()));
            fixture
                .ledger
                .expect_get_environment()
                .returning_st(Environment::default);

            fixture
                .daemon
                .expect_get_env()
                .returning_st(|| Ok(Environment::default()));
            fixture.daemon.expect_daemon_check().return_const(true);
            fixture.daemon.expect_daemon_can_open().return_const(true);

            // The status mock returns daemon service entries only for service
            // types tracked in `provisioned_services`. This simulates the daemon's
            // reconciler: a service only appears in the status after it has been
            // provisioned. Test helpers (expect_create_user, etc.) add entries to
            // `provisioned_services` when they simulate successful onchain txs.
            let status_provisioned = fixture.provisioned_services.clone();
            fixture.daemon.expect_status().returning_st(move || {
                let provisioned = status_provisioned.lock().unwrap();
                let mut statuses = Vec::new();
                for (user_type, tunnel_name, tunnel_dst, dz_ip) in [
                    ("IBRL", "doublezero1", "5.6.7.1", "10.1.1.1"),
                    ("IBRLWithAllocatedIP", "doublezero1", "5.6.7.1", "10.1.1.1"),
                    ("EdgeFiltering", "doublezero1", "5.6.7.1", "10.1.1.1"),
                    ("Multicast", "doublezero2", "5.6.7.2", "10.1.1.2"),
                ] {
                    if provisioned.contains(user_type) {
                        statuses.push(StatusResponse {
                            doublezero_status: DoubleZeroStatus {
                                session_status: "BGP Session Up".to_string(),
                                last_session_update: Some(0),
                            },
                            tunnel_name: Some(tunnel_name.to_string()),
                            tunnel_src: Some("1.2.3.4".to_string()),
                            tunnel_dst: Some(tunnel_dst.to_string()),
                            doublezero_ip: Some(dz_ip.to_string()),
                            user_type: Some(user_type.to_string()),
                        });
                    }
                }
                Ok(statuses)
            });

            let latencies = fixture.latencies.clone();
            fixture.daemon.expect_latency().returning_st(move || {
                let results = latencies.lock().unwrap().clone();
                let ready = !results.is_empty();
                Ok(LatencyResponse { ready, results })
            });

            let accesspass = fixture.accesspass.clone();
            fixture
                .ledger
                .expect_get_accesspass()
                .with(
                    predicate::eq(Ipv4Addr::new(1, 2, 3, 4)),
                    predicate::eq(payer),
                )
                .returning_st(move |_, _| Ok(Some(accesspass.lock().unwrap().clone())));

            let users = fixture.users.clone();
            fixture
                .ledger
                .expect_list_user()
                .returning_st(move || Ok(users.lock().unwrap().clone()));

            let devices = fixture.devices.clone();
            fixture
                .ledger
                .expect_list_device()
                .returning_st(move || Ok(devices.lock().unwrap().clone()));

            let exchanges = fixture.exchanges.clone();
            fixture
                .ledger
                .expect_list_exchange()
                .returning_st(move || Ok(exchanges.lock().unwrap().clone()));

            let mcast_groups = fixture.mcast_groups.clone();
            fixture
                .ledger
                .expect_list_multicastgroup()
                .returning_st(move || Ok(mcast_groups.lock().unwrap().clone()));

            let feeds = fixture.feeds.clone();
            fixture
                .ledger
                .expect_list_feed()
                .returning_st(move || Ok(feeds.lock().unwrap().clone()));

            let users = fixture.users.clone();
            fixture
                .ledger
                .expect_get_user()
                .returning_st(move |user_pk| {
                    thread::sleep(Duration::from_secs(1));
                    let users = users.lock().unwrap();
                    match users.get(&user_pk) {
                        Some(user) => Ok(user.clone()),
                        None => Err(eyre::eyre!("User not found")),
                    }
                });

            let devices = fixture.devices.clone();
            fixture
                .ledger
                .expect_get_device()
                .returning_st(move |pubkey_or_code| {
                    thread::sleep(Duration::from_secs(1));
                    let devices = devices.lock().unwrap();
                    match parse_pubkey(&pubkey_or_code) {
                        Some(pk) => match devices.get(&pk) {
                            Some(device) => Ok(device.clone()),
                            None => Err(eyre::eyre!("Invalid Account Type")),
                        },
                        None => {
                            let dev = devices.iter().find(|(_, v)| v.code == pubkey_or_code);
                            match dev {
                                Some((_, device)) => Ok(device.clone()),
                                None => Err(eyre::eyre!("Device not found")),
                            }
                        }
                    }
                });

            let tenants = fixture.tenants.clone();
            fixture
                .ledger
                .expect_get_tenant()
                .returning_st(move |pubkey_or_code| {
                    let tenants = tenants.lock().unwrap();
                    match parse_pubkey(&pubkey_or_code) {
                        Some(pk) => match tenants.get(&pk) {
                            Some(tenant) => Ok((pk, tenant.clone())),
                            None => Err(eyre::eyre!("Invalid Account Type")),
                        },
                        None => {
                            let tenant = tenants.iter().find(|(_, v)| v.code == pubkey_or_code);
                            match tenant {
                                Some((pk, tenant)) => Ok((*pk, tenant.clone())),
                                None => Err(eyre::eyre!("Tenant not found")),
                            }
                        }
                    }
                });

            fixture
        }

        pub fn setup_enable<F: Fn() -> eyre::Result<()> + Send + 'static>(&mut self, f: F) {
            self.daemon.expect_enable().returning(f);
        }

        pub fn add_device(
            &mut self,
            device_type: DeviceType,
            latency_ns: i64,
            reachable: bool,
        ) -> (Pubkey, Device) {
            let mut devices = self.devices.lock().unwrap();
            let device_number = devices.len() + 1;
            let pk = Pubkey::new_unique();
            let device_ip = format!("5.6.7.{device_number}");
            self.latencies.lock().unwrap().push(LatencyRecord {
                device_pk: pk.to_string(),
                device_ip: device_ip.clone(),
                device_code: format!("device{device_number}"),
                min_latency_ns: latency_ns,
                max_latency_ns: latency_ns,
                avg_latency_ns: latency_ns,
                reachable,
            });
            // A real, resolvable exchange per device, cycling through actual metro names so
            // tests can assert on them — a device code conventionally leads with the metro, but
            // that is a naming convention, not the data the display code under test resolves.
            const TEST_METRO_NAMES: [&str; 6] = [
                "Amsterdam",
                "Frankfurt",
                "Singapore",
                "New York",
                "London",
                "Tokyo",
            ];
            let exchange_pk = Pubkey::new_unique();
            self.exchanges.lock().unwrap().insert(
                exchange_pk,
                Exchange {
                    account_type: AccountType::Exchange,
                    owner: Pubkey::new_unique(),
                    index: device_number as u128,
                    bump_seed: 1,
                    lat: 0.0,
                    lng: 0.0,
                    bgp_community: 0,
                    unused: 0,
                    status: ExchangeStatus::Activated,
                    code: format!("x{device_number}"),
                    name: TEST_METRO_NAMES[(device_number - 1) % TEST_METRO_NAMES.len()]
                        .to_string(),
                    reference_count: 0,
                    device1_pk: Pubkey::default(),
                    device2_pk: Pubkey::default(),
                },
            );
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: device_number as u128,
                bump_seed: 255,
                reference_count: 0,
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk,
                device_type,
                public_ip: device_ip.parse().unwrap(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                code: format!("device{device_number}"),
                dz_prefixes: format!("10.{}.0.0/24", device_number).parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
                unicast_users_count: 0,
                multicast_subscribers_count: 0,
                max_unicast_users: 0,
                max_multicast_subscribers: 0,
                reserved_seats: 0,
                multicast_publishers_count: 0,
                max_multicast_publishers: 0,
                ..Default::default()
            };
            devices.insert(pk, device.clone());
            (pk, device)
        }

        pub fn add_multicast_group(
            &mut self,
            code: &str,
            multicast_ip: &str,
        ) -> (Pubkey, MulticastGroup) {
            let mut mcast_groups = self.mcast_groups.lock().unwrap();
            let pk = Pubkey::new_unique();
            let group = MulticastGroup {
                account_type: AccountType::MulticastGroup,
                owner: Pubkey::new_unique(),
                index: 1,
                bump_seed: 1,
                tenant_pk: Pubkey::new_unique(),
                multicast_ip: multicast_ip.parse().unwrap(),
                max_bandwidth: 10_000_000_000,
                status: MulticastGroupStatus::Activated,
                code: code.to_string(),
                publisher_count: 0,
                subscriber_count: 0,
            };
            mcast_groups.insert(pk, group.clone());
            (pk, group)
        }

        pub fn add_tenant(&mut self, code: &str) -> (Pubkey, Tenant) {
            let mut tenants = self.tenants.lock().unwrap();
            let pk = Pubkey::new_unique();
            let tenant = Tenant {
                account_type: AccountType::Tenant,
                owner: Pubkey::new_unique(),
                bump_seed: 1,
                code: code.to_string(),
                vrf_id: 100,
                reference_count: 0,
                administrators: vec![],
                payment_status: TenantPaymentStatus::Paid,
                token_account: Pubkey::default(),
                metro_routing: false,
                route_liveness: false,
                billing: TenantBillingConfig::default(),
                include_topologies: vec![],
            };
            tenants.insert(pk, tenant.clone());
            (pk, tenant)
        }

        pub fn create_user(
            &mut self,
            user_type: UserType,
            device_pk: Pubkey,
            client_ip: &str,
        ) -> User {
            // Look up device's public_ip to set as tunnel_endpoint
            let tunnel_endpoint = self
                .devices
                .lock()
                .unwrap()
                .get(&device_pk)
                .map(|d| d.public_ip)
                .unwrap_or(Ipv4Addr::UNSPECIFIED);

            User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 1,
                bump_seed: 1,
                user_type,
                device_pk,
                tenant_pk: Pubkey::new_unique(),
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: client_ip.parse().unwrap(),
                dz_ip: client_ip.parse().unwrap(),
                tunnel_id: 1,
                tunnel_net: "10.1.1.0/31".parse().unwrap(),
                status: UserStatus::Activated,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::new_unique(),
                tunnel_endpoint,
                tunnel_flags: 0,
                bgp_status: Default::default(),
                last_bgp_up_at: 0,
                last_bgp_reported_at: 0,
                bgp_rtt_ns: 0,
                ..Default::default()
            }
        }

        pub fn add_user(&mut self, user: &User) -> Pubkey {
            let mut users = self.users.lock().unwrap();
            let pk = Pubkey::new_unique();
            users.insert(pk, user.clone());
            let users = self.users.clone();
            self.ledger
                .expect_list_user()
                .returning_st(move || Ok(users.lock().unwrap().clone()));
            pk
        }

        pub fn expect_create_user(&mut self, pk: Pubkey, user: &User) {
            self.expect_create_user_with_tenant(pk, user, Some(self.default_tenant_pk));
        }

        pub fn expect_create_user_with_tenant(
            &mut self,
            pk: Pubkey,
            user: &User,
            tenant_pk: Option<Pubkey>,
        ) {
            self.expect_create_user_with_tenant_and_proof(pk, user, tenant_pk, None);
        }

        /// The RFC-27 variant: asserts the exact proof the command carries, so a test can pin
        /// that the proof the verification service returned is the one that reaches the SDK.
        pub fn expect_create_user_with_tenant_and_proof(
            &mut self,
            pk: Pubkey,
            user: &User,
            tenant_pk: Option<Pubkey>,
            ip_proof: Option<IpOwnershipProof>,
        ) {
            let expected_create_user_command = CreateUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                tunnel_endpoint: user.tunnel_endpoint,
                tenant_pk,
                ip_proof,
            };

            let users = self.users.clone();
            let provisioned = self.provisioned_services.clone();
            let user = user.clone();
            self.ledger
                .expect_create_user()
                .times(1)
                .with(predicate::eq(expected_create_user_command))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    let ut = user.user_type.to_string();
                    users.lock().unwrap().insert(pk, user.clone());
                    provisioned.lock().unwrap().insert(ut);
                    Ok(pk)
                });
        }

        pub fn expect_create_subscribe_user(
            &mut self,
            pk: Pubkey,
            user: &User,
            mgroup_pks: Vec<Pubkey>,
            publisher: bool,
            subscriber: bool,
        ) {
            self.expect_create_subscribe_user_with_proof(
                pk, user, mgroup_pks, publisher, subscriber, None,
            );
        }

        /// The RFC-27 variant: asserts the exact proof the command carries.
        #[allow(clippy::too_many_arguments)]
        pub fn expect_create_subscribe_user_with_proof(
            &mut self,
            pk: Pubkey,
            user: &User,
            mgroup_pks: Vec<Pubkey>,
            publisher: bool,
            subscriber: bool,
            ip_proof: Option<IpOwnershipProof>,
        ) {
            let expected_create_subscribe_user_command = CreateSubscribeUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                mgroup_pks: mgroup_pks.clone(),
                publisher,
                subscriber,
                tunnel_endpoint: user.tunnel_endpoint,
                owner: None,
                feed_pk: None,
                ip_proof,
            };

            let users = self.users.clone();
            let provisioned = self.provisioned_services.clone();
            let mut user = user.clone();
            if publisher {
                user.publishers.extend(&mgroup_pks);
            }
            if subscriber {
                user.subscribers.extend(&mgroup_pks);
            }
            self.ledger
                .expect_create_subscribe_user()
                .times(1)
                .with(predicate::eq(expected_create_subscribe_user_command))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    let ut = user.user_type.to_string();
                    users.lock().unwrap().insert(pk, user.clone());
                    provisioned.lock().unwrap().insert(ut);
                    Ok(pk)
                });
        }

        pub fn expect_update_multicastgroup_roles(
            &mut self,
            user_pk: Pubkey,
            group_pks: Vec<Pubkey>,
            client_ip: Ipv4Addr,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_command = UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pks,
                client_ip,
                publisher,
                subscriber,
                device_pk: None,
                feed_pk: None,
            };

            let users = self.users.clone();
            let provisioned = self.provisioned_services.clone();
            self.ledger
                .expect_update_multicastgroup_roles()
                .times(1)
                .with(predicate::eq(expected_command))
                .returning_st(move |cmd| {
                    thread::sleep(Duration::from_secs(1));
                    let mut users = users.lock().unwrap();
                    if let Some(user) = users.get_mut(&cmd.user_pk) {
                        if cmd.publisher {
                            user.publishers.extend(&cmd.group_pks);
                        }
                        if cmd.subscriber {
                            user.subscribers.extend(&cmd.group_pks);
                        }
                        provisioned
                            .lock()
                            .unwrap()
                            .insert(user.user_type.to_string());
                    }
                    Ok(())
                });
        }

        /// Turn the fixture pass into an EdgeSeat pass seating exactly `feed_pks`, which the
        /// join preflight in resolve_feed_join demands.
        pub fn seat_feeds(&mut self, feed_pks: &[Pubkey]) {
            self.accesspass.lock().unwrap().accesspass_type = AccessPassType::EdgeSeat(
                feed_pks
                    .iter()
                    .map(|pk| FeedSeat {
                        feed_key: *pk,
                        max_users: 2,
                        ..Default::default()
                    })
                    .collect(),
            );
        }

        /// Turn the fixture pass into an EdgeSeat pass with an explicit cap and live count per
        /// feed, as `(feed_pk, max_users, current_users)`. `seat_feeds` fixes every cap at 2,
        /// which cannot express a full seat or two feeds with different caps.
        pub fn seat_feeds_with_caps(&mut self, seats: &[(Pubkey, u8, u8)]) {
            self.accesspass.lock().unwrap().accesspass_type = AccessPassType::EdgeSeat(
                seats
                    .iter()
                    .map(|(feed_pk, max_users, current_users)| FeedSeat {
                        feed_key: *feed_pk,
                        max_users: *max_users,
                        current_users: *current_users,
                        ..Default::default()
                    })
                    .collect(),
            );
        }

        /// Turn the fixture pass into an EdgeSeat pass carrying no feeds but explicit
        /// per-category user seats, as `(live_count, max)`. These four counters are the
        /// ones `try_add_user` enforces, and `seat_feeds*` cannot reach them.
        pub fn edge_seat_user_caps(&mut self, unicast: (u16, u16), multicast: (u16, u16)) {
            let mut ap = self.accesspass.lock().unwrap();
            ap.accesspass_type = AccessPassType::EdgeSeat(vec![]);
            (ap.unicast_user_count, ap.max_unicast_users) = unicast;
            (ap.multicast_user_count, ap.max_multicast_users) = multicast;
        }

        pub fn add_feed(&mut self, code: &str, exchange: Pubkey, groups: Vec<Pubkey>) -> Pubkey {
            let pk = Pubkey::new_unique();
            let feed = Feed {
                account_type: AccountType::Feed,
                owner: Pubkey::new_unique(),
                bump_seed: 1,
                code: code.to_string(),
                name: code.to_string(),
                exchange,
                groups,
                ..Default::default()
            };
            self.feeds.lock().unwrap().insert(pk, feed);
            pk
        }

        pub fn expect_subscribe_feed(&mut self, user_pk: Pubkey, feed_pks: Vec<Pubkey>) {
            let expected_command = SubscribeFeedCommand { user_pk, feed_pks };

            let users = self.users.clone();
            let feeds = self.feeds.clone();
            let provisioned = self.provisioned_services.clone();
            self.ledger
                .expect_subscribe_feed()
                .times(1)
                .with(predicate::eq(expected_command))
                .returning_st(move |cmd| {
                    let mut users = users.lock().unwrap();
                    let feeds = feeds.lock().unwrap();
                    if let Some(user) = users.get_mut(&cmd.user_pk) {
                        for feed_pk in &cmd.feed_pks {
                            user.feed_pks.push(*feed_pk);
                            if let Some(feed) = feeds.get(feed_pk) {
                                for group in &feed.groups {
                                    if !user.subscribers.contains(group) {
                                        user.subscribers.push(*group);
                                    }
                                }
                            }
                        }
                        provisioned
                            .lock()
                            .unwrap()
                            .insert(user.user_type.to_string());
                    }
                    Ok(())
                });
        }

        pub fn expect_unsubscribe_feed(&mut self, user_pk: Pubkey, feed_pks: Vec<Pubkey>) {
            let expected_command = UnsubscribeFeedCommand { user_pk, feed_pks };

            let users = self.users.clone();
            self.ledger
                .expect_unsubscribe_feed()
                .times(1)
                .with(predicate::eq(expected_command))
                .returning_st(move |cmd| {
                    let mut users = users.lock().unwrap();
                    if let Some(user) = users.get_mut(&cmd.user_pk) {
                        user.feed_pks.retain(|pk| !cmd.feed_pks.contains(pk));
                    }
                    Ok(())
                });
        }
    }

    /// Run `connect` against the fixture's mocks with a captured writer,
    /// returning the result and the writer output.
    ///
    /// Defaults to an environment with no verification service, which is the pre-RFC-27
    /// behavior every test below other than the proof tests is asserting.
    async fn run(fixture: &TestFixture, command: Connect) -> (eyre::Result<()>, String) {
        run_with_proof_client(fixture, command, no_verifier()).await
    }

    /// The same, with a verification service the test controls.
    async fn run_with_proof_client(
        fixture: &TestFixture,
        command: Connect,
        proof_client: MockIpProofClient,
    ) -> (eyre::Result<()>, String) {
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let result = command
            .execute_with_proof_client(
                &ctx,
                &fixture.daemon,
                &fixture.ledger,
                &proof_client,
                &mut out,
            )
            .await;
        (result, String::from_utf8(out).unwrap())
    }

    /// An environment with no verifier configured — what every environment looks like until the
    /// deployment work lands.
    fn no_verifier() -> MockIpProofClient {
        let mut client = MockIpProofClient::new();
        client
            .expect_request_proof()
            .returning(|_, _, _| Err(IpProofError::NotConfigured));
        client
    }

    /// A verifier that signs `client_ip` for whatever it is asked, recording the `user_type` it
    /// was asked for so a test can assert the proof is bound to the user actually being created.
    fn verifier_for(client_ip: Ipv4Addr) -> (MockIpProofClient, Arc<Mutex<Vec<UserType>>>) {
        let seen = Arc::new(Mutex::new(Vec::new()));
        let recorded = seen.clone();
        let mut client = MockIpProofClient::new();
        client
            .expect_request_proof()
            .returning_st(move |payer, user_type, _source| {
                recorded.lock().unwrap().push(user_type);
                Ok(test_proof(payer, client_ip, user_type))
            });
        (client, seen)
    }

    /// A proof with a placeholder signature: nothing client-side verifies it, and the program
    /// side is covered by the serviceability tests.
    fn test_proof(payer: Pubkey, client_ip: Ipv4Addr, user_type: UserType) -> IpOwnershipProof {
        IpOwnershipProof {
            version: 1,
            payer,
            client_ip,
            epoch: 10,
            user_type: user_type as u8,
            signature: [3u8; 64],
        }
    }

    #[test]
    fn test_connect_command_ibrl_hybrid() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Create a tenant for this test
            let (tenant_pk, tenant) = fixture.add_tenant("my-tenant");

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok());
            assert!(output.contains("Using tenant 'my-tenant' from CLI argument."));
            assert!(output.contains("Device selected: device1"));
            assert!(output.contains("✅  User Provisioned"));

            // Adding a multicast tunnel with an existing IBRL creates a separate Multicast user
            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");

            // When IBRL user exists, a separate Multicast user should be created on a different device
            // (exclude_ips prevents reusing the same device as the IBRL tunnel)
            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![mcast_group_pk],
                true,  // publisher
                false, // subscriber
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok());
            assert!(output.contains("Creating separate Multicast user for concurrent tunnels"));
        });
    }

    // ========================================================================
    // RFC-27 IP ownership proof (issue #4201)
    // ========================================================================

    /// An IBRL connect where the verification service answers: the proof it signed is the one
    /// that reaches the SDK, and it is bound to the `user_type` being created.
    #[test]
    fn test_connect_attaches_the_ip_proof_the_service_issued() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (tenant_pk, tenant) = fixture.add_tenant("proof-tenant");
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);

            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let payer = fixture.ledger.get_payer();
            let proof = test_proof(payer, user.client_ip, UserType::IBRL);
            fixture.expect_create_user_with_tenant_and_proof(
                Pubkey::new_unique(),
                &user,
                Some(tenant_pk),
                Some(proof),
            );

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                ..Default::default()
            };

            let (verifier, user_types) = verifier_for(user.client_ip);
            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert!(
                output.contains("IP ownership verified for 1.2.3.4"),
                "{output}"
            );
            // The proof binds `user_type`, so a proof requested for the wrong one would be
            // rejected onchain even though every other field matched.
            assert_eq!(*user_types.lock().unwrap(), vec![UserType::IBRL]);
        });
    }

    /// A multicast connect asks for a proof bound to `Multicast`, not to the IBRL type the
    /// unicast path uses.
    #[test]
    fn test_connect_binds_the_proof_to_the_multicast_user_type() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (mcast_group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);

            let mcast_user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let payer = fixture.ledger.get_payer();
            fixture.expect_create_subscribe_user_with_proof(
                Pubkey::new_unique(),
                &mcast_user,
                vec![mcast_group_pk],
                true,
                false,
                Some(test_proof(payer, mcast_user.client_ip, UserType::Multicast)),
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                ..Default::default()
            };

            let (verifier, user_types) = verifier_for(mcast_user.client_ip);
            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert_eq!(*user_types.lock().unwrap(), vec![UserType::Multicast]);
        });
    }

    /// An unreachable service must not take a host offline. Creation proceeds without a proof,
    /// which the program accepts until enforcement is switched on — and the operator is told.
    #[test]
    fn test_connect_continues_without_a_proof_when_the_service_is_unreachable() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (tenant_pk, tenant) = fixture.add_tenant("proof-tenant");
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);

            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            // `expect_create_user_with_tenant` pins `ip_proof: None`, so this test fails if the
            // fallback ever starts attaching something.
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                ..Default::default()
            };

            let mut verifier = MockIpProofClient::new();
            verifier.expect_request_proof().returning(|_, _, _| {
                Err(IpProofError::Unreachable {
                    url: "http://ip-verifier.example:8080/v1/proof".to_string(),
                    detail: "connection refused".to_string(),
                })
            });

            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert!(output.contains("could not reach"), "{output}");
            assert!(
                output.contains("Continuing without an IP ownership proof"),
                "{output}"
            );
        });
    }

    /// A CGNAT or RFC-1918 source address is a refusal, not an outage. The operator needs the
    /// service's own reason, because the remedy is theirs, not ours.
    #[test]
    fn test_connect_reports_a_non_routable_source_address_and_continues() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (tenant_pk, tenant) = fixture.add_tenant("proof-tenant");
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);

            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                ..Default::default()
            };

            let mut verifier = MockIpProofClient::new();
            verifier.expect_request_proof().returning(|_, _, _| {
                Err(IpProofError::Declined {
                    reason: "not_globally_routable".to_string(),
                    message: "100.64.3.9 is not a globally routable address".to_string(),
                })
            });

            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert!(output.contains("not_globally_routable"), "{output}");
            assert!(output.contains("100.64.3.9"), "{output}");
        });
    }

    /// ...but only where a user is actually created. A host whose user already exists attaches
    /// nothing and binds nothing, so a disagreeing verifier must not take it offline — the
    /// re-activation it came for has nothing to do with the proof.
    #[test]
    fn test_connect_ignores_a_disagreeing_service_when_the_user_already_exists() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let mut user =
                fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
            user.status = UserStatus::Activated;
            fixture.add_user(&user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: true,
                }),
                ..Default::default()
            };

            // The same disagreement that stops a creation. Nothing must ask for the proof here,
            // so the mock is set up never to be called at all.
            let mut verifier = MockIpProofClient::new();
            verifier.expect_request_proof().never();

            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert!(
                output.contains("An account already exists with Pubkey:"),
                "{output}"
            );
        });
    }

    /// The one hard failure: the service saw a different address than the one being provisioned.
    /// Attaching the proof would be rejected onchain and dropping it would bind an address nobody
    /// proved, so `connect` stops and names both.
    #[test]
    fn test_connect_fails_when_the_service_saw_a_different_address() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (_, tenant) = fixture.add_tenant("proof-tenant");
            fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);
            // No create_user expectation: reaching the ledger at all is the failure.

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                ..Default::default()
            };

            // The daemon reports 1.2.3.4; the service egressed by another path and saw 9.9.9.9.
            let (verifier, _) = verifier_for(Ipv4Addr::new(9, 9, 9, 9));
            let (result, output) = run_with_proof_client(&fixture, command, verifier).await;

            let err = result.expect_err("a proof for another address must stop the connect");
            let message = err.to_string();
            assert!(message.contains("9.9.9.9"), "{message}");
            assert!(message.contains("1.2.3.4"), "{message}");
            assert!(output.contains("9.9.9.9"), "{output}");
            assert!(output.contains("1.2.3.4"), "{output}");
        });
    }

    #[test]
    fn test_connect_command_ibrl_edge() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Create a tenant for this test
            let (tenant_pk, tenant) = fixture.add_tenant("edge-tenant");

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Edge, 100, true);
            // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Edge, 110, true);
            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());

            // Adding a multicast tunnel with an existing IBRL creates a separate Multicast user
            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");

            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![mcast_group_pk],
                true,  // publisher
                false, // subscriber
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_ibrl_transit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Transit, 100, true);
            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            // Should fail because Transit devices are not allowed for IBRL
            assert!(result.is_err());
        });
    }

    #[test]
    fn test_connect_command_ibrl_allocate_hybrid() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Create a tenant for this test
            let (tenant_pk, tenant) = fixture.add_tenant("allocate-tenant");

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: true,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_ibrl_allocate_edge() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Create a tenant for this test
            let (tenant_pk, tenant) = fixture.add_tenant("edge-allocate-tenant");

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Edge, 100, true);
            let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: true,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_ibrl_allocate_transit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Transit, 100, true);
            let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: true,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            // Should fail because Transit devices are not allowed for IBRL with allocate_addr
            assert!(result.is_err());
        });
    }

    #[test]
    fn test_connect_banned_user() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let mut user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            user.status = UserStatus::Banned;
            fixture.add_user(&user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_err());
            assert!(output.contains("❌  The user is banned."));
        });
    }

    #[test]
    fn test_connect_command_multicast_publisher() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                true,
                false,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok());
            assert!(output.contains("✅  User Provisioned"));
        });
    }

    /// `connect multicast` with no groups auto-joins every group authorized in the
    /// AccessPass: publishes to mgroup_pub_allowlist and subscribes to mgroup_sub_allowlist.
    #[test]
    fn test_connect_command_multicast_autojoin_from_accesspass() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (g1_pk, _) = fixture.add_multicast_group("group-1", "239.0.0.1");
            let (g2_pk, _) = fixture.add_multicast_group("group-2", "239.0.0.2");

            // Authorize publishing to g1 and subscribing to g1 + g2.
            {
                let mut ap = fixture.accesspass.lock().unwrap();
                ap.mgroup_pub_allowlist = vec![g1_pk];
                ap.mgroup_sub_allowlist = vec![g1_pk, g2_pk];
            }

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

            // First group (g1) created via create_subscribe_user as publisher + subscriber.
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_subscribe_user(user_pk, &user, vec![g1_pk], true, true);
            // Remaining group (g2) added via update_multicastgroup_roles as subscriber-only.
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                vec![g2_pk],
                Ipv4Addr::new(1, 2, 3, 4),
                false,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "auto-join from access pass must succeed: {:?}",
                result.err()
            );
            assert!(output.contains("Publishing to (from AccessPass): group-1"));
            assert!(output.contains("Subscribing to (from AccessPass): group-1, group-2"));
        });
    }

    /// When every authorized group shares the same (publisher, subscriber) flag pair,
    /// all of them fold into the single CreateSubscribeUser transaction and no
    /// follow-up role update is issued.
    #[test]
    fn test_connect_command_multicast_single_transaction_when_flags_match() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (g1_pk, _) = fixture.add_multicast_group("group-1", "239.0.0.1");
            let (g2_pk, _) = fixture.add_multicast_group("group-2", "239.0.0.2");

            // Subscribe-only for both groups → one shared flag pair.
            {
                let mut ap = fixture.accesspass.lock().unwrap();
                ap.mgroup_sub_allowlist = vec![g1_pk, g2_pk];
            }

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

            // Both groups ride in the create, in allowlist order. No
            // expect_update_multicastgroup_roles: any such call would panic the mock.
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_subscribe_user(user_pk, &user, vec![g1_pk, g2_pk], false, true);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "single-transaction auto-join must succeed: {:?}",
                result.err()
            );
            assert!(output.contains("Subscribing to (from AccessPass): group-1, group-2"));
        });
    }

    /// Auto-join is a no-op success when the AccessPass authorizes no groups: no user
    /// is created and no subscriptions are issued.
    #[test]
    fn test_connect_command_multicast_autojoin_empty_allowlist_is_noop() {
        block_on(async {
            let mut fixture = TestFixture::new();
            // AccessPass has empty allowlists by default; a device exists but must not be used.
            fixture.add_device(DeviceType::Hybrid, 100, true);

            // No expect_create_subscribe_user / expect_update_multicastgroup_roles: any such
            // call would panic the mock.

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "empty allowlist must be a no-op success: {:?}",
                result.err()
            );
            assert!(output.contains("has no authorized multicast groups"));
            assert!(!output.contains("✅  User Provisioned"));
        });
    }

    /// Allowlist entries that no longer resolve to a known multicast group are dropped
    /// during auto-join; only the still-valid groups are used.
    #[test]
    fn test_connect_command_multicast_autojoin_filters_stale_allowlist_pubkeys() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (g1_pk, _) = fixture.add_multicast_group("group-1", "239.0.0.1");
            // Not registered in list_multicastgroup — simulates a deleted group.
            let stale_pk = Pubkey::new_unique();

            {
                let mut ap = fixture.accesspass.lock().unwrap();
                ap.mgroup_sub_allowlist = vec![stale_pk, g1_pk];
            }

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

            // Only g1 survives filtering → single create_subscribe_user as subscriber-only,
            // no further update calls.
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_subscribe_user(user_pk, &user, vec![g1_pk], false, true);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "stale allowlist pubkeys must be filtered: {:?}",
                result.err()
            );
        });
    }

    /// `parse_dz_mode` accepts multicast with no groups, yielding empty pub/sub vectors
    /// that trigger the AccessPass-driven auto-join downstream.
    #[test]
    fn test_parse_dz_mode_multicast_no_args_yields_empty_groups() {
        let command = Connect {
            dz_mode: Some(DzMode::Multicast {
                mode: None,
                multicast_groups: vec![],
                pub_groups: vec![],
                sub_groups: vec![],
                sub_feeds: vec![],
                unsub_feeds: vec![],
            }),
            client_ip: None,
            device: None,
            ip_verifier_url: None,
            verbose: false,
            ..Default::default()
        };

        match command.parse_dz_mode().unwrap() {
            ParsedDzMode::Multicast {
                pub_groups,
                sub_groups,
            } => {
                assert!(pub_groups.is_empty());
                assert!(sub_groups.is_empty());
            }
            _ => panic!("expected ParsedDzMode::Multicast"),
        }
    }

    /// Multicast connect succeeds when the AccessPass has last_access_epoch = 0 (expired).
    /// Multicast access is gated by mgroup_*_allowlist, not by epoch.
    #[test]
    fn test_connect_command_multicast_publisher_with_expired_accesspass() {
        block_on(async {
            let mut fixture = TestFixture::new();
            // Expire the access pass (last_access_epoch < current_epoch). The CLI must NOT
            // reject the connect for multicast.
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                true,
                false,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec!["test-group".to_string()],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "multicast connect must succeed with expired access pass, got: {:?}",
                result.err()
            );
        });
    }

    /// Multicast subscriber connect succeeds with expired access pass — symmetric to publisher.
    #[test]
    fn test_connect_command_multicast_subscriber_with_expired_accesspass() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                false,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group".to_string()],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "multicast subscriber connect must succeed with expired access pass: {:?}",
                result.err()
            );
        });
    }

    /// Existing IBRL user adding a multicast subscription with expired access pass succeeds.
    /// Exercises the `(Some(ibrl), None)` branch of `find_or_create_user_and_subscribe`:
    /// a separate Multicast user is created via CreateSubscribeUser on a different device.
    #[test]
    fn test_add_multicast_to_existing_ibrl_user_with_expired_accesspass() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);

            // Existing IBRL user on device1
            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let _ibrl_user_pk = fixture.add_user(&ibrl_user);

            // Expect a new Multicast user to be created on device2 (concurrent tunnels = different device)
            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![mcast_group_pk],
                false,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group".to_string()],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "adding multicast to existing IBRL user must succeed with expired access pass: {:?}",
                result.err()
            );
        });
    }

    /// Existing multicast user subscribes to a new group with expired access pass.
    /// Exercises the `(_, Some(mcast))` branch of `find_or_create_user_and_subscribe`,
    /// which calls UpdateMulticastGroupRoles (the onchain processor never had an epoch
    /// check; this test verifies the CLI gate no longer blocks it either).
    /// A group requested in BOTH --publish and --subscribe on an existing Multicast
    /// user gets one update with both flags, instead of a publisher add that a later
    /// subscriber-only write strips (the instruction sets absolute role state).
    #[test]
    fn test_connect_existing_user_group_in_both_lists_keeps_both_roles() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

            // Existing multicast user with no roles yet.
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            let user_pk = fixture.add_user(&user);

            // Exactly ONE update carrying both roles.
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                vec![mcast_group_pk],
                user.client_ip,
                true,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec!["test-group".to_string()],
                    sub_groups: vec!["test-group".to_string()],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "both-lists connect on existing user must succeed: {:?}",
                result.err()
            );
        });
    }

    #[test]
    fn test_connect_command_multicast_add_group_to_existing_user_with_expired_accesspass() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (mcast_group2_pk, _mcast_group2) =
                fixture.add_multicast_group("test-group2", "239.0.0.2");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

            // Existing multicast user already subscribed to test-group
            let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            user.subscribers.push(mcast_group_pk);
            let user_pk = fixture.add_user(&user);

            // Expect UpdateMulticastGroupRoles for the new group
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                vec![mcast_group2_pk],
                user.client_ip,
                false,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group2".to_string()],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "adding new group to existing multicast user must succeed with expired access pass: {:?}",
                result.err()
            );
        });
    }

    /// Regression: IBRL connect still fails when the AccessPass has last_access_epoch < current_epoch.
    #[test]
    fn test_connect_command_ibrl_with_expired_accesspass_fails() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;

            let (_device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(
                result.is_err(),
                "IBRL connect must still fail with expired access pass"
            );
            let err = result.unwrap_err().to_string();
            assert!(
                err.contains("AccessPass"),
                "expected AccessPass error, got: {err}"
            );
            assert!(output.contains("Unable to find a valid AccessPass"));
        });
    }

    async fn execute_multicast_test_succeed_adding_second_group(multicast_mode: MulticastMode) {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, _mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (mcast_group2_pk, _mcast_group2) =
            fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

        let (publisher, subscriber) = match multicast_mode {
            MulticastMode::Publisher => (true, false),
            MulticastMode::Subscriber => (false, true),
        };

        // User already has first group
        if multicast_mode == MulticastMode::Subscriber {
            user.subscribers.push(mcast_group_pk);
        } else {
            user.publishers.push(mcast_group_pk);
        }

        let user_pk = fixture.add_user(&user);

        // Expect subscribe to second group
        fixture.expect_update_multicastgroup_roles(
            user_pk,
            vec![mcast_group2_pk],
            user.client_ip,
            publisher,
            subscriber,
        );

        let command = Connect {
            dz_mode: Some(DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group2".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
                sub_feeds: vec![],
                unsub_feeds: vec![],
            }),
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            ip_verifier_url: None,
            verbose: false,
            ..Default::default()
        };

        let (result, _) = run(&fixture, command).await;
        assert!(result.is_ok());
    }

    #[test]
    fn test_connect_command_multicast_publisher_rejects_duplicate_groups() {
        block_on(async {
            let mut fixture = TestFixture::new();

            fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    // Pass the same group twice — should error
                    multicast_groups: vec!["test-group".to_string(), "test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_err());
            assert!(result
                .unwrap_err()
                .to_string()
                .contains("Duplicate multicast pub group"));
        });
    }

    #[test]
    fn test_connect_command_multicast_publisher_can_add_subscriber_group() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (mcast_group2_pk, _mcast_group2) =
                fixture.add_multicast_group("test-group2", "239.0.0.2");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

            // Create a user who is already a publisher
            let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            user.publishers.push(mcast_group_pk);
            let user_pk = fixture.add_user(&user);

            // Expect update_multicastgroup_roles call for the new subscriber group
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                vec![mcast_group2_pk],
                user.client_ip,
                false,
                true,
            );

            // Add subscriber group to existing publisher - should succeed
            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group2".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_multicast_subscriber_can_add_publisher_group() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (mcast_group2_pk, _mcast_group2) =
                fixture.add_multicast_group("test-group2", "239.0.0.2");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

            // Create a user who is already a subscriber
            let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            user.subscribers.push(mcast_group_pk);
            let user_pk = fixture.add_user(&user);

            // Expect update_multicastgroup_roles call for the new publisher group
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                vec![mcast_group2_pk],
                user.client_ip,
                true,
                false,
            );

            // Add publisher group to existing subscriber - should succeed
            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group2".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_multicast_publisher_succeed_adding_second_group() {
        block_on(execute_multicast_test_succeed_adding_second_group(
            MulticastMode::Publisher,
        ));
    }

    #[test]
    fn test_connect_command_multicast_subscriber_succeed_adding_second_group() {
        block_on(execute_multicast_test_succeed_adding_second_group(
            MulticastMode::Subscriber,
        ));
    }

    async fn execute_multicast_test_succeed_already_in_the_group(multicast_mode: MulticastMode) {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, _mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

        if multicast_mode == MulticastMode::Subscriber {
            user.subscribers.push(mcast_group_pk);
        } else {
            user.publishers.push(mcast_group_pk);
        }

        fixture.add_user(&user);

        let command = Connect {
            dz_mode: Some(DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
                sub_feeds: vec![],
                unsub_feeds: vec![],
            }),
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            ip_verifier_url: None,
            verbose: false,
            ..Default::default()
        };

        let (result, _) = run(&fixture, command).await;
        assert!(result.is_ok());
    }

    #[test]
    fn test_connect_command_multicast_publisher_succeed_already_in_group() {
        block_on(execute_multicast_test_succeed_already_in_the_group(
            MulticastMode::Publisher,
        ));
    }

    #[test]
    fn test_connect_command_multicast_subscriber_succeed_already_in_group() {
        block_on(execute_multicast_test_succeed_already_in_the_group(
            MulticastMode::Subscriber,
        ));
    }

    #[test]
    fn test_connect_command_multicast_subscribe() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            (_, _) = fixture.add_multicast_group("test-group2", "239.0.0.2");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            // Add a second device for concurrent tunnels (Multicast + IBRL must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                false,
                true,
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());

            // Test that adding an IBRL tunnel with an existing multicast succeeds on a DIFFERENT device
            // (concurrent tunnels from same client IP must go to different devices)
            let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
            fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_command_delayed_latencies() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            (_, _) = fixture.add_multicast_group("test-group2", "239.0.0.2");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            // Add a second device for concurrent tunnels (Multicast + IBRL must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
            let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                false,
                true,
            );

            // Save device latencies for delayed availability test
            let latency_record_device1 = fixture.latencies.lock().unwrap()[0].clone();
            let latency_record_device2 = fixture.latencies.lock().unwrap()[1].clone();
            fixture.latencies.lock().unwrap().clear();
            let latencies = Arc::clone(&fixture.latencies);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            // Inject the latency results only after connect has started polling.
            let injector = tokio::task::spawn(async move {
                tokio::time::sleep(Duration::from_secs(2)).await;
                let mut latencies = latencies.lock().unwrap();
                latencies.push(latency_record_device1);
                latencies.push(latency_record_device2);
            });

            let (result1, _) = run(&fixture, command).await;
            injector.await.unwrap();

            assert!(result1.is_ok());

            // IBRL user should go to a DIFFERENT device than the existing Multicast user
            // (concurrent tunnels from same client IP must go to different devices)
            let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
            fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());
        });
    }

    #[test]
    fn test_connect_to_device_at_max_users() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Add a device with max_users = 0
            let (device_pk, mut device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device.max_users = 0;
            device.users_count = 0;

            // Update the device in the mock
            fixture
                .devices
                .lock()
                .unwrap()
                .insert(device_pk, device.clone());

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: Some(device.code.clone()), // Explicitly specify the device
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;

            assert!(result.is_err());
            let err_msg = result.unwrap_err().to_string();
            assert!(
                err_msg.contains("Device is not accepting more users"),
                "Expected error about device not accepting users, got: {}",
                err_msg
            );
            assert!(
                !err_msg.contains("Device not found"),
                "Should not show 'Device not found' error when device exists but is full"
            );
        });
    }

    #[test]
    fn test_connect_to_device_at_capacity() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Add a device that's at capacity (users_count >= max_users)
            let (device_pk, mut device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device.max_users = 10;
            device.users_count = 10; // At capacity

            // Update the device in the mock
            fixture
                .devices
                .lock()
                .unwrap()
                .insert(device_pk, device.clone());

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: Some(device.code.clone()), // Explicitly specify the device
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;

            assert!(result.is_err());
            let err_msg = result.unwrap_err().to_string();
            assert!(
                err_msg.contains("Device is not accepting more users"),
                "Expected error about device not accepting users, got: {}",
                err_msg
            );
        });
    }

    #[test]
    fn test_auto_select_skips_device_at_unicast_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // First device: at unicast user limit
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_unicast_users = 5;
            device1.unicast_users_count = 5;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            // Second device: has capacity (higher latency, but the only eligible device)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 200, true);
            let user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
            fixture.expect_create_user(Pubkey::new_unique(), &user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: None, // auto-select
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            // The mock expects create_user to be called with device2_pk (via expect_create_user).
            // If device1 is incorrectly selected, the mock predicate mismatch causes Err, caught here.
            assert!(
                result.is_ok(),
                "Expected success selecting device2 (device1 is at unicast limit)"
            );
        });
    }

    #[test]
    fn test_auto_select_skips_device_at_multicast_publisher_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");

            // First device: at multicast publisher limit
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_multicast_publishers = 48;
            device1.multicast_publishers_count = 48;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            // Second device: has capacity
            let (device2_pk, _) = fixture.add_device(DeviceType::Hybrid, 200, true);
            let user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                true,  // publisher
                false, // not subscriber
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None, // auto-select
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            // The mock expects create_subscribe_user with device2_pk; if device1 is selected the mock fails.
            assert!(
                result.is_ok(),
                "Expected success selecting device2 (device1 is at publisher limit)"
            );
        });
    }

    #[test]
    fn test_auto_select_skips_device_at_multicast_subscriber_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");

            // First device: at multicast subscriber limit
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_multicast_subscribers = 10;
            device1.multicast_subscribers_count = 10;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            // Second device: has capacity
            let (device2_pk, _) = fixture.add_device(DeviceType::Hybrid, 200, true);
            let user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![mcast_group_pk],
                false, // not publisher
                true,  // subscriber
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            // The mock expects create_subscribe_user with device2_pk; if device1 is selected the mock fails.
            assert!(
                result.is_ok(),
                "Expected success selecting device2 (device1 is at subscriber limit)"
            );
        });
    }

    #[test]
    fn test_auto_select_fails_when_all_devices_at_multicast_publisher_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            fixture.add_multicast_group("test-group", "239.0.0.1");

            // Only device: at multicast publisher limit
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_multicast_publishers = 48;
            device1.multicast_publishers_count = 48;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_err(),
                "Expected error when no devices have capacity"
            );
        });
    }

    #[test]
    fn test_auto_select_fails_when_all_devices_at_multicast_subscriber_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            fixture.add_multicast_group("test-group", "239.0.0.1");

            // Only device: at multicast subscriber limit but has free IBRL slots
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_multicast_subscribers = 10;
            device1.multicast_subscribers_count = 10;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_err(),
                "Expected error when no devices have multicast subscriber capacity"
            );
        });
    }

    #[test]
    fn test_auto_select_fails_when_all_devices_at_unicast_limit() {
        block_on(async {
            let mut fixture = TestFixture::new();

            // Only device: at unicast limit but has free multicast slots
            let (device1_pk, mut device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device1.max_unicast_users = 5;
            device1.unicast_users_count = 5;
            fixture.devices.lock().unwrap().insert(device1_pk, device1);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_err(),
                "Expected error when no devices have unicast capacity"
            );
        });
    }

    #[test]
    fn test_connect_to_nonexistent_device() {
        block_on(async {
            let mut fixture = TestFixture::new();

            fixture.add_device(DeviceType::Hybrid, 100, true); // Add a device, but we'll try to connect to a different one

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some("1.2.3.4".to_string()),
                device: Some("nonexistent-device".to_string()), // Device that doesn't exist
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;

            assert!(result.is_err());
            let err_msg = result.unwrap_err().to_string();
            assert!(
                err_msg.contains("Device not found"),
                "Expected 'Device not found' error for nonexistent device, got: {}",
                err_msg
            );
        });
    }

    #[test]
    fn test_connect_command_ibrl_allocate_existing_user() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let mut user =
                fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
            user.status = UserStatus::Activated;
            fixture.add_user(&user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: true,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok());
            assert!(output.contains("An account already exists with Pubkey:"));
        });
    }

    /// Test that adding multicast to an existing IBRL user creates a separate Multicast user
    ///
    /// This test verifies that when multicast is added to an IP that already has an IBRL user,
    /// the system creates a new Multicast user (enabling concurrent unicast + multicast tunnels).
    #[test]
    fn test_add_multicast_to_existing_ibrl_user() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);

            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let _ibrl_user_pk = fixture.add_user(&ibrl_user);

            // Expect create_subscribe_user to be called to create a NEW Multicast user on a DIFFERENT device
            // (concurrent tunnels from same client IP must go to different devices)
            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![mcast_group_pk],
                false, // publisher
                true,  // subscriber
            );

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec![],
                }),
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "Adding multicast to existing IBRL user should succeed by creating separate Multicast user: {:?}",
                result.err()
            );
        });
    }

    /// Test that multiple user types per IP are properly isolated
    ///
    /// This test verifies that:
    /// 1. A Multicast user exists for an IP
    /// 2. An IBRL user can be created for the same IP (different UserType = different PDA)
    /// 3. The two users are independent
    #[test]
    fn test_multiple_user_types_per_ip_isolation() {
        block_on(async {
            let mut fixture = TestFixture::new();

            let (mcast_group_pk, _mcast_group) =
                fixture.add_multicast_group("test-group", "239.0.0.1");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
            let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);

            // Create a pure Multicast user and add it to the fixture (simulating existing user)
            let mut mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            mcast_user.subscribers.push(mcast_group_pk);
            let mcast_user_pk = fixture.add_user(&mcast_user);

            // Create an IBRL user for the same IP on a DIFFERENT device
            // (concurrent tunnels from same client IP must go to different devices)
            let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
            fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                }),
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "IBRL user creation should succeed even with existing Multicast user for same IP: {:?}",
                result.err()
            );

            // Verify both users exist for the same IP
            let users = fixture.users.lock().unwrap();
            let users_for_ip: Vec<_> = users
                .values()
                .filter(|u| u.client_ip == ibrl_user.client_ip)
                .collect();
            assert_eq!(
                users_for_ip.len(),
                2,
                "Should have 2 users for the same IP (one IBRL, one Multicast), mcast_pk={}, users={:?}",
                mcast_user_pk,
                users_for_ip.iter().map(|u| u.user_type).collect::<Vec<_>>()
            );
        });
    }

    /// Test that connect completes even when enable() fails.
    /// When the reconciler can't be enabled and isn't already enabled,
    /// the connect flow skips tunnel polling and returns early.
    #[test]
    fn test_connect_enable_failure_is_nonfatal() {
        block_on(async {
            let mut fixture = TestFixture::new_with_failing_enable();

            let (tenant_pk, tenant) = fixture.add_tenant("my-tenant");
            let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, Some(tenant_pk));

            let command = Connect {
                dz_mode: Some(DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                }),
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(
                result.is_ok(),
                "Connect should succeed even when enable() fails: {:?}",
                result.err()
            );
            assert!(output.contains("failed to enable reconciler"));
        });
    }

    /// `--subscribe-feed` on a fresh IP creates a bare Multicast user (no group, no tenant), then
    /// joins the feed with one SubscribeFeed covering it.
    #[test]
    fn test_connect_command_subscribe_feed_creates_bare_user() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (g0, g1) = (Pubkey::new_unique(), Pubkey::new_unique());
            let feed_pk = fixture.add_feed("shreds", device.exchange_pk, vec![g0, g1]);
            fixture.seat_feeds(&[feed_pk]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![feed_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["shreds".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[5], "    Joined feed(s): shreds", "{output}");
        });
    }

    /// `--subscribe-feed` with an existing activated Multicast user skips creation and goes
    /// straight to the subscription. No create expectation is set, so a create call would panic.
    #[test]
    fn test_connect_command_subscribe_feed_existing_user_skips_creation() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let feed_pk = fixture.add_feed("shreds", device.exchange_pk, vec![g0]);
            fixture.seat_feeds(&[feed_pk]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let user_pk = fixture.add_user(&user);
            fixture.expect_subscribe_feed(user_pk, vec![feed_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["shreds".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(
                lines[3],
                format!("    An account already exists with Pubkey: {user_pk}"),
                "{output}"
            );
            assert_eq!(lines[4], "    Joined feed(s): shreds", "{output}");
        });
    }

    /// `--unsubscribe-feed` leaves a held feed with one UnsubscribeFeed and creates nothing.
    #[test]
    fn test_connect_command_unsubscribe_feed() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let feed_pk = fixture.add_feed("shreds", device.exchange_pk, vec![g0]);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = vec![feed_pk];
            user.subscribers = vec![g0];
            let user_pk = fixture.add_user(&user);
            fixture.expect_unsubscribe_feed(user_pk, vec![feed_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![],
                    unsub_feeds: vec!["shreds".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[3], "    Left feed(s): shreds", "{output}");
        });
    }

    /// A feed whose metro differs from the user's device fails before any transaction: no
    /// subscribe_feed expectation is set, so a send would panic the mock.
    #[test]
    fn test_connect_command_subscribe_feed_wrong_metro_rejected() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, _device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_feed("away", Pubkey::new_unique(), vec![Pubkey::new_unique()]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            fixture.add_user(&user);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["away".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "feed away does not serve the metro of device device1; run 'doublezero disconnect multicast' first to reconnect on a device in the feed's metro"
            );
        });
    }

    /// On a fresh IP the feeds choose the metro; with no eligible device there, the command fails
    /// before creating anything (no create_user expectation is set, so a create would panic).
    #[test]
    fn test_connect_command_subscribe_feed_wrong_metro_creates_nothing() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_feed("away", Pubkey::new_unique(), vec![Pubkey::new_unique()]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["away".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "no eligible device serves the metro of the requested feed(s)"
            );
        });
    }

    /// The feeds choose the metro on a fresh IP: the feed's device wins over a lower-latency
    /// device in another metro.
    #[test]
    fn test_connect_command_subscribe_feed_picks_a_device_in_the_feed_metro() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.add_device(DeviceType::Hybrid, 100, true);
            let (far_pk, far) = fixture.add_device(DeviceType::Hybrid, 500, true);
            let g0 = Pubkey::new_unique();
            let feed_pk = fixture.add_feed("shreds", far.exchange_pk, vec![g0]);
            fixture.seat_feeds(&[feed_pk]);

            let user = fixture.create_user(UserType::Multicast, far_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![feed_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["shreds".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[3], "    Device selected: device2", "{output}");
        });
    }

    /// A bare `connect multicast`: no mode, no groups, no feed flags.
    fn bare_multicast() -> Connect {
        Connect {
            dz_mode: Some(DzMode::Multicast {
                mode: None,
                multicast_groups: vec![],
                pub_groups: vec![],
                sub_groups: vec![],
                sub_feeds: vec![],
                unsub_feeds: vec![],
            }),
            ..Default::default()
        }
    }

    /// A bare `doublezero connect`: no subcommand at all.
    fn bare_connect(client_ip: &str) -> Connect {
        Connect {
            client_ip: Some(client_ip.to_string()),
            ..Default::default()
        }
    }

    /// Machine A: a bare connect on a pass carrying two purchased feeds joins both, one seat each.
    #[test]
    fn test_connect_command_bare_joins_every_purchased_feed_with_headroom() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            let kalshi_pk =
                fixture.add_feed("kalshi", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 0), (kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            // Code order: kalshi before shreds.
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk, shreds_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Joined feed(s): kalshi, shreds"),
                "{output}"
            );
        });
    }

    /// Machine B: kalshi is full, shreds has headroom. Joins shreds alone and exits 0.
    #[test]
    fn test_connect_command_bare_joins_only_the_feed_with_a_free_seat() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            let kalshi_pk =
                fixture.add_feed("kalshi", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 1), (kalshi_pk, 1, 1)]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![shreds_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Joined feed(s): shreds"),
                "{output}"
            );
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Skipped, no free seat: kalshi (1 of 1 seats in use)"),
                "{output}"
            );
        });
    }

    /// Machine C: every purchased feed is full and this machine holds none, so the connect fails
    /// and creates nothing. No create_user or subscribe_feed expectation is set, so either call
    /// would panic the mock.
    #[test]
    fn test_connect_command_bare_fails_when_every_seat_is_full() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (_device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            let kalshi_pk =
                fixture.add_feed("kalshi", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 2), (kalshi_pk, 1, 1)]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "every purchased feed is already at capacity: kalshi (1 of 1 seats in use), shreds (2 of 2 seats in use). Free a seat by disconnecting another machine, or buy more.",
                "{output}"
            );
        });
    }

    /// A machine that already holds a feed succeeds on a re-run even though it joins nothing and
    /// the remaining feed is full. This is what makes repeated installs safe, and it must still
    /// activate the user: a re-run whose reconciler is disabled must not exit 0 while leaving the
    /// tunnel unprovisioned.
    #[test]
    fn test_connect_command_bare_is_a_no_op_when_already_holding_a_feed() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            let kalshi_pk =
                fixture.add_feed("kalshi", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 1), (kalshi_pk, 1, 1)]);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = vec![shreds_pk];
            fixture.add_user(&user);
            // The tunnel was already provisioned on a previous run, so the re-run's
            // user_activated poll finds it immediately rather than timing out.
            fixture
                .provisioned_services
                .lock()
                .unwrap()
                .insert("Multicast".to_string());

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Device selected: device1"),
                "{output}"
            );
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Already joined: shreds"),
                "{output}"
            );
            assert!(output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// Purchased feeds exist but no eligible device serves that metro: fail before selecting a
    /// device at all, naming the feed the operator was trying to reach.
    #[test]
    fn test_connect_command_bare_fails_when_no_eligible_device_serves_the_feeds_metro() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.add_device(DeviceType::Hybrid, 100, true); // exists, but in a different metro
            let away_exchange = Pubkey::new_unique();
            let kalshi_pk = fixture.add_feed("kalshi", away_exchange, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "no eligible device serves the metro of purchased feed(s) with a free seat: kalshi. Connect from a machine in that metro.",
                "{output}"
            );
        });
    }

    /// Zero eligible devices exist anywhere, unrelated to any feed's metro: the pre-existing
    /// device-selection error must surface, not a metro-specific message that would mask it.
    #[test]
    fn test_connect_command_bare_surfaces_the_device_error_when_no_eligible_device_exists_at_all() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, mut device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            device.max_users = 0; // ineligible: no eligible device exists anywhere
            fixture
                .devices
                .lock()
                .unwrap()
                .insert(device_pk, device.clone());
            let kalshi_pk =
                fixture.add_feed("kalshi", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "No activated devices found",
                "{output}"
            );
        });
    }

    /// Mixed case: one purchased feed is already full, and the other has headroom but no
    /// eligible device serves its metro. The failure must name both facts, not just one.
    #[test]
    fn test_connect_command_bare_names_both_the_full_feed_and_the_deviceless_metro() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (_device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            let away_exchange = Pubkey::new_unique(); // no device exists in this metro
            let kalshi_pk = fixture.add_feed("kalshi", away_exchange, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 2), (kalshi_pk, 1, 0)]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "no eligible device serves the metro of purchased feed(s) with a free seat: kalshi; also already at capacity: shreds (2 of 2 seats in use). Connect from a machine in that metro.",
                "{output}"
            );
        });
    }

    /// A purchased feed in another metro is skipped with a bare code list, not the fuller
    /// per-feed comparison — that full phrase belongs only to the failure message, since here the
    /// "another metro" header and the "Device selected" line two above it already give context.
    #[test]
    fn test_connect_command_bare_prints_a_bare_code_list_for_a_skipped_other_metro_feed() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (near_pk, near_device) = fixture.add_device(DeviceType::Hybrid, 10, true);
            let (_far_pk, far_device) = fixture.add_device(DeviceType::Hybrid, 500, true);
            let kalshi_pk = fixture.add_feed(
                "kalshi",
                near_device.exchange_pk,
                vec![Pubkey::new_unique()],
            );
            let wombat_pk =
                fixture.add_feed("wombat", far_device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0), (wombat_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, near_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Skipped, another metro: wombat"),
                "{output}"
            );
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Joined feed(s): kalshi"),
                "{output}"
            );
        });
    }

    /// A device in the feed's metro is chosen over a nearer device outside it: latency alone
    /// must never route past the metro a purchased feed actually needs.
    #[test]
    fn test_connect_command_bare_joins_via_a_device_in_the_feeds_metro_even_when_nearer_elsewhere()
    {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.add_device(DeviceType::Hybrid, 10, true); // nearest, wrong metro
            let (far_pk, far_device) = fixture.add_device(DeviceType::Hybrid, 500, true); // feed's metro
            let kalshi_pk =
                fixture.add_feed("kalshi", far_device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, far_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Device selected: device2"),
                "{output}"
            );
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Joined feed(s): kalshi"),
                "{output}"
            );
        });
    }

    /// An excluded device that is meaningfully faster earns an informational notice — an upsell
    /// signal only, never implying the excluded device could have carried this connection today.
    #[test]
    fn test_connect_command_bare_notes_lower_latency_when_excluded_device_is_meaningfully_faster() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (_fast_pk, fast_device) = fixture.add_device(DeviceType::Hybrid, 5_000_000, true);
            let (far_pk, far_device) = fixture.add_device(DeviceType::Hybrid, 20_000_000, true);
            let kalshi_pk =
                fixture.add_feed("kalshi", far_device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, far_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let fast_metro = fixture
                .exchanges
                .lock()
                .unwrap()
                .get(&fast_device.exchange_pk)
                .unwrap()
                .name
                .clone();
            assert!(
                output.lines().any(|line| line
                    == format!(
                    "ℹ️  Lower latency is available from {} in {fast_metro} (5.00ms vs 20.00ms)",
                    fast_device.code
                )),
                "{output}"
            );
        });
    }

    /// A metro name is cosmetic: when the excluded device's exchange cannot be resolved (e.g.
    /// deleted after the device was created), the notice still prints, just with a bare device
    /// code, and the connect still succeeds — a resolve failure here must never fail a connect.
    #[test]
    fn test_connect_command_bare_omits_the_metro_name_when_the_exchange_is_unresolvable() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (_fast_pk, fast_device) = fixture.add_device(DeviceType::Hybrid, 5_000_000, true);
            let (far_pk, far_device) = fixture.add_device(DeviceType::Hybrid, 20_000_000, true);
            fixture
                .exchanges
                .lock()
                .unwrap()
                .remove(&fast_device.exchange_pk);
            let kalshi_pk =
                fixture.add_feed("kalshi", far_device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, far_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output.lines().any(|line| line
                    == format!(
                        "ℹ️  Lower latency is available from {} (5.00ms vs 20.00ms)",
                        fast_device.code
                    )),
                "{output}"
            );
        });
    }

    /// A difference under the 1ms threshold is noise, not a genuine gap, so no notice appears.
    #[test]
    fn test_connect_command_bare_omits_lower_latency_notice_under_threshold() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.add_device(DeviceType::Hybrid, 20_000_000, true);
            let (far_pk, far_device) = fixture.add_device(DeviceType::Hybrid, 20_500_000, true);
            let kalshi_pk =
                fixture.add_feed("kalshi", far_device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, far_pk, "1.2.3.4");
            let user_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(user_pk, &user, None);
            fixture.expect_subscribe_feed(user_pk, vec![kalshi_pk]);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(!output.contains("Lower latency is available"), "{output}");
        });
    }

    /// The post-device metro-mismatch failure is still reachable on the existing-user path,
    /// where the device is fixed rather than chosen from the feeds' candidate metros.
    #[test]
    fn test_connect_command_bare_fails_when_existing_users_device_metro_has_no_purchased_feed() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true); // Amsterdam
            let (_away_pk, away_device) = fixture.add_device(DeviceType::Hybrid, 200, true); // Frankfurt
            let kalshi_pk = fixture.add_feed(
                "kalshi",
                away_device.exchange_pk,
                vec![Pubkey::new_unique()],
            );
            fixture.seat_feeds_with_caps(&[(kalshi_pk, 1, 0)]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            fixture.add_user(&user);

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                format!(
                    "no purchased feed serves the metro of device {}: kalshi is served from Frankfurt. Connect from a machine in Frankfurt.",
                    device.code
                ),
                "{output}"
            );
        });
    }

    /// --device naming a different device than the existing Multicast user's is rejected, not
    /// silently ignored — mirrors the guard `resolve_feed_join` applies on the explicit-feed path.
    #[test]
    fn test_connect_command_bare_fails_when_device_flag_names_a_different_device() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 200, true);
            let shreds_pk =
                fixture.add_feed("shreds", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds_with_caps(&[(shreds_pk, 2, 0)]);

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            let user_pk = fixture.add_user(&user);

            let mut command = bare_multicast();
            command.device = Some("device2".to_string());

            let (result, output) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                format!(
                    "existing Multicast user {user_pk} is on device device1; run 'doublezero disconnect multicast' first, or omit --device"
                ),
                "{output}"
            );
        });
    }

    /// Regression guard: a non-EdgeSeat pass still takes the multicast-allowlist auto-join path,
    /// unchanged. Without this, the new branch could silently capture every existing pass.
    #[test]
    fn test_connect_command_bare_non_edge_seat_pass_still_uses_the_allowlist() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, _device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (group_pk, _group) = fixture.add_multicast_group("test-group", "239.0.0.1");
            // Prepaid pass (the fixture default) with one subscriber-allowlisted group.
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &user,
                vec![group_pk],
                false,
                true,
            );

            let (result, output) = run(&fixture, bare_multicast()).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            assert!(
                output
                    .lines()
                    .any(|line| line == "    Subscribing to (from AccessPass): test-group"),
                "{output}"
            );
        });
    }

    /// Feed flags cannot be combined with group flags, even on a programmatically built command
    /// (clap's conflicts_with_all rejects the same mix at parse time).
    #[test]
    fn test_connect_command_feed_flags_conflict_with_group_flags() {
        block_on(async {
            let fixture = TestFixture::new();

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["group".to_string()],
                    sub_feeds: vec!["feed".to_string()],
                    unsub_feeds: vec![],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "Cannot mix --subscribe-feed/--unsubscribe-feed with --publish/--subscribe or positional group arguments"
            );
        });
    }

    /// The same feed in both flags is rejected up front.
    #[test]
    fn test_connect_command_feed_in_both_flags_rejected() {
        block_on(async {
            let fixture = TestFixture::new();

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["shreds".to_string()],
                    unsub_feeds: vec!["shreds".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                "feed shreds is in both --subscribe-feed and --unsubscribe-feed"
            );
        });
    }

    /// The same feed as a code in one flag and its pubkey in the other is caught after both
    /// resolve, before any transaction: no unsubscribe or subscribe expectation is set.
    #[test]
    fn test_connect_command_feed_in_both_flags_by_pubkey_rejected() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let feed_pk = fixture.add_feed("shreds", device.exchange_pk, vec![g0]);
            fixture.seat_feeds(&[feed_pk]);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = vec![feed_pk];
            user.subscribers = vec![g0];
            fixture.add_user(&user);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec![feed_pk.to_string()],
                    unsub_feeds: vec!["shreds".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, _) = run(&fixture, command).await;
            assert_eq!(
                result.unwrap_err().to_string(),
                format!("feed {feed_pk} is in both --subscribe-feed and --unsubscribe-feed")
            );
        });
    }

    /// A swap at the per-user feed cap works: the cap counts the post-leave state, so leaving one
    /// feed and joining another in one command sends both transactions.
    #[test]
    fn test_connect_command_feed_swap_at_the_cap() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let old_pk = fixture.add_feed("old", device.exchange_pk, vec![g0]);
            let g_new = Pubkey::new_unique();
            let new_pk = fixture.add_feed("new", device.exchange_pk, vec![g_new]);
            let fillers: Vec<Pubkey> = (0..MAX_USER_FEEDS - 1)
                .map(|_| Pubkey::new_unique())
                .collect();
            let mut seated = vec![old_pk, new_pk];
            seated.extend(fillers.iter().copied());
            fixture.seat_feeds(&seated);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = std::iter::once(old_pk).chain(fillers).collect();
            user.subscribers = vec![g0];
            let user_pk = fixture.add_user(&user);
            fixture.expect_unsubscribe_feed(user_pk, vec![old_pk]);
            fixture.expect_subscribe_feed(user_pk, vec![new_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["new".to_string()],
                    unsub_feeds: vec!["old".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}\n{output}", result.err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[4], "    Left feed(s): old", "{output}");
            assert_eq!(lines[5], "    Joined feed(s): new", "{output}");
        });
    }

    /// A swap runs the leave first; when the join half then fails, the output names the half that
    /// failed and says to rerun only that flag.
    #[test]
    fn test_connect_command_feed_swap_reports_failed_half() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let old_pk = fixture.add_feed("old", device.exchange_pk, vec![g0]);
            let new_pk = fixture.add_feed("new", device.exchange_pk, vec![Pubkey::new_unique()]);
            fixture.seat_feeds(&[old_pk, new_pk]);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = vec![old_pk];
            user.subscribers = vec![g0];
            let user_pk = fixture.add_user(&user);
            fixture.expect_unsubscribe_feed(user_pk, vec![old_pk]);
            fixture
                .ledger
                .expect_subscribe_feed()
                .times(1)
                .returning_st(|_| Err(eyre::eyre!("feed seat is full")));

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["new".to_string()],
                    unsub_feeds: vec!["old".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[4], "    Left feed(s): old", "{output}");
            assert_eq!(
                lines[5], "❌  --subscribe-feed failed: feed seat is full",
                "{output}"
            );
            assert_eq!(
                lines[6],
                "    --unsubscribe-feed succeeded. Rerun with only --subscribe-feed new to finish.",
                "{output}"
            );
        });
    }

    /// When the leave half fails after validation but the join succeeds, the daemon still gets
    /// its provisioning work before the error is reported.
    #[test]
    fn test_connect_command_feed_swap_leave_failure_still_provisions_the_join() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let g0 = Pubkey::new_unique();
            let old_pk = fixture.add_feed("old", device.exchange_pk, vec![g0]);
            let g_new = Pubkey::new_unique();
            let new_pk = fixture.add_feed("new", device.exchange_pk, vec![g_new]);
            fixture.seat_feeds(&[old_pk, new_pk]);

            let mut user = fixture.create_user(UserType::Multicast, device_pk, "1.2.3.4");
            user.feed_pks = vec![old_pk];
            user.subscribers = vec![g0];
            let user_pk = fixture.add_user(&user);
            fixture
                .ledger
                .expect_unsubscribe_feed()
                .times(1)
                .returning_st(|_| Err(eyre::eyre!("rpc timed out")));
            fixture.expect_subscribe_feed(user_pk, vec![new_pk]);

            let command = Connect {
                dz_mode: Some(DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                    sub_feeds: vec!["new".to_string()],
                    unsub_feeds: vec!["old".to_string()],
                }),
                client_ip: None,
                device: None,
                ip_verifier_url: None,
                verbose: false,
                ..Default::default()
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_err());
            let lines: Vec<&str> = output.lines().collect();
            assert_eq!(lines[8], "    Session: BGP Session Up", "{output}");
            assert_eq!(
                lines[10],
                "    --subscribe-feed succeeded. Rerun with only --unsubscribe-feed old to finish.",
                "{output}"
            );
        });
    }

    // ---- Bare `doublezero connect`: everything the AccessPass authorizes ----

    /// The headline case: one command, both tunnels. Also pins the leg ordering —
    /// the multicast leg must see the fresh IBRL user and pick a different device.
    /// A bare `connect` provisions two users of two different `user_type`s, and the proof binds
    /// `user_type` — so one proof cannot cover both legs. Each leg must get its own, or whichever
    /// leg the single proof did not name would be refused onchain.
    #[test]
    fn test_connect_bare_takes_a_proof_per_leg() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (device2_pk, _) = fixture.add_device(DeviceType::Hybrid, 110, true);

            let client_ip = Ipv4Addr::new(1, 2, 3, 4);
            let payer = fixture.ledger.get_payer();

            // Each expectation pins the proof its own creation must carry, so a proof bound to
            // the other leg's `user_type` fails the test rather than the chain.
            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant_and_proof(
                Pubkey::new_unique(),
                &ibrl_user,
                None,
                Some(test_proof(payer, client_ip, UserType::IBRL)),
            );

            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user_with_proof(
                Pubkey::new_unique(),
                &mcast_user,
                vec![group_pk],
                false, // publisher
                true,  // subscriber
                Some(test_proof(payer, client_ip, UserType::Multicast)),
            );

            let (verifier, seen) = verifier_for(client_ip);
            let (result, output) =
                run_with_proof_client(&fixture, bare_connect("1.2.3.4"), verifier).await;

            assert!(result.is_ok(), "{result:?}\n{output}");
            assert!(output.contains("IBRL: provisioned"), "{output}");
            assert!(output.contains("Multicast: provisioned"), "{output}");

            // Two requests, one per leg, each naming the user type that leg creates.
            assert_eq!(
                *seen.lock().unwrap(),
                vec![UserType::IBRL, UserType::Multicast],
                "{output}"
            );
        });
    }

    #[test]
    fn test_connect_bare_provisions_ibrl_and_multicast() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (device2_pk, _) = fixture.add_device(DeviceType::Hybrid, 110, true);

            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &ibrl_user, None);

            let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![group_pk],
                false, // publisher
                true,  // subscriber
            );

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(output.contains("IBRL: provisioned"), "{output}");
            assert!(output.contains("Multicast: provisioned"), "{output}");
            assert!(output.contains("✅  User Provisioned"), "{output}");
            assert!(
                output.contains("Creating separate Multicast user for concurrent tunnels"),
                "{output}"
            );
        });
    }

    /// A pass with no multicast grants connects IBRL and says so, rather than failing.
    /// Any `create_subscribe_user` call would panic the mock.
    #[test]
    fn test_connect_bare_ibrl_only_when_accesspass_has_no_groups() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);

            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &ibrl_user, None);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(output.contains("IBRL: provisioned"), "{output}");
            assert!(
                output.contains("The AccessPass has no authorized multicast groups"),
                "{output}"
            );
            assert!(output.contains("Multicast: nothing authorized"), "{output}");
            assert!(output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// Epoch expiry gates unicast only, so the multicast leg still runs. Any
    /// `create_user` call would panic the mock.
    #[test]
    fn test_connect_bare_expired_accesspass_skips_ibrl_runs_multicast() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![group_pk],
                false,
                true,
            );

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(output.contains("IBRL skipped"), "{output}");
            assert!(
                output.contains("expired at epoch 0 (current 10)"),
                "{output}"
            );
            assert!(output.contains("Multicast: provisioned"), "{output}");
            assert!(output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// Nothing was provisioned and nothing failed. Exiting 0 here would read as success
    /// to an unattended installer, so this must be an error.
    #[test]
    fn test_connect_bare_expired_accesspass_and_no_groups_is_an_error() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;
            fixture.add_device(DeviceType::Hybrid, 100, true);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            let err = result.expect_err("nothing was provisioned; must not exit 0");
            assert!(format!("{err:#}").contains("nothing to connect"), "{err:#}");
            assert!(!output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// The reconciler is turned on even on the run that provisions nothing — otherwise a
    /// skipped run silently leaves the daemon unmanaged.
    #[test]
    fn test_connect_bare_enables_reconciler_even_when_nothing_to_connect() {
        block_on(async {
            let mut fixture = TestFixture::new();
            fixture.accesspass.lock().unwrap().last_access_epoch = 0;
            fixture.add_device(DeviceType::Hybrid, 100, true);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_err());
            assert!(output.contains("Reconciler enabled"), "{output}");
        });
    }

    /// A daemon that will not enable must not stop the onchain user being created —
    /// the operator can run `doublezero enable` afterwards.
    #[test]
    fn test_connect_bare_continues_when_enable_fails() {
        block_on(async {
            let mut fixture = TestFixture::new_with_failing_enable();
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &ibrl_user, None);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(
                output.contains("Failed to enable the reconciler"),
                "{output}"
            );
            assert!(output.contains("IBRL: provisioned"), "{output}");
        });
    }

    /// An EdgeSeat pass with its unicast seats full skips the IBRL leg up front rather
    /// than letting `try_add_user` reject the transaction onchain.
    #[test]
    fn test_connect_bare_edge_seat_unicast_cap_exhausted_skips_ibrl() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            let feed_pk = fixture.add_feed("shreds", device1.exchange_pk, vec![group_pk]);

            fixture.edge_seat_user_caps((1, 1), (0, 1));
            fixture.seat_feeds(&[feed_pk]);

            let mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            let mcast_pk = Pubkey::new_unique();
            fixture.expect_create_user_with_tenant(mcast_pk, &mcast_user, None);
            fixture.expect_subscribe_feed(mcast_pk, vec![feed_pk]);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(output.contains("IBRL skipped"), "{output}");
            assert!(
                output.contains("no free unicast seat (1 of 1 in use)"),
                "{output}"
            );
            assert!(output.contains("Multicast: provisioned"), "{output}");
        });
    }

    /// The seat cap is only charged on create, so a machine already holding both users
    /// must re-run cleanly even though its pass reports every seat in use. Any create
    /// call would panic the mock.
    #[test]
    fn test_connect_bare_edge_seat_reruns_cleanly_when_users_exist() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            let feed_pk = fixture.add_feed("shreds", device1.exchange_pk, vec![group_pk]);

            fixture.edge_seat_user_caps((1, 1), (1, 1));
            fixture.seat_feeds(&[feed_pk]);

            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.add_user(&ibrl_user);
            let mut mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            mcast_user.feed_pks = vec![feed_pk];
            mcast_user.subscribers = vec![group_pk];
            fixture.add_user(&mcast_user);
            fixture
                .provisioned_services
                .lock()
                .unwrap()
                .insert(UserType::IBRL.to_string());
            fixture
                .provisioned_services
                .lock()
                .unwrap()
                .insert(UserType::Multicast.to_string());

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(!output.contains("IBRL skipped"), "{output}");
            assert!(output.contains("IBRL: provisioned"), "{output}");
        });
    }

    /// A failing IBRL leg must not abort the multicast leg, and the summary must name
    /// the command that finishes the half that did not land.
    #[test]
    fn test_connect_bare_ibrl_failure_still_attempts_multicast() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            fixture.add_device(DeviceType::Hybrid, 110, true);

            fixture
                .ledger
                .expect_create_user()
                .returning(|_| Err(eyre::eyre!("simulated create_user failure")));

            let mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
            fixture.expect_create_subscribe_user(
                Pubkey::new_unique(),
                &mcast_user,
                vec![group_pk],
                false,
                true,
            );

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_err());
            assert!(output.contains("❌  IBRL failed"), "{output}");
            assert!(
                output.contains("Rerun 'doublezero connect ibrl' to finish."),
                "{output}"
            );
            assert!(output.contains("Multicast: provisioned"), "{output}");
            // The success banner must not contradict the failure or the nonzero exit.
            assert!(!output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// With only one eligible device the IBRL leg takes it and `exclude_ips` starves the
    /// multicast leg. The IBRL tunnel still stands, but the command reports failure — and
    /// must not advise a rerun, which would recompute the same exclusion and fail again.
    #[test]
    fn test_connect_bare_multicast_failure_after_ibrl_success() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &ibrl_user, None);

            let (result, output) = run(&fixture, bare_connect("1.2.3.4")).await;
            assert!(result.is_err(), "{output}");
            assert!(output.contains("IBRL: provisioned"), "{output}");
            assert!(output.contains("❌  Multicast failed"), "{output}");
            assert!(
                output.contains("Rerunning 'doublezero connect multicast' will fail the same way"),
                "{output}"
            );
            assert!(
                output.contains("Free an endpoint with 'doublezero disconnect ibrl'"),
                "{output}"
            );
            assert!(
                !output.contains("Rerun 'doublezero connect multicast' to finish."),
                "a rerun cannot clear the exclusion that caused this: {output}"
            );
            assert!(!output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// `--device` skips `best_latency`, so a starved device reaches `select_tunnel_endpoint`'s
    /// UNSPECIFIED fallback. That value is legal onchain and the controller reads it as the
    /// device public IP, so writing it would silently put the multicast tunnel on the same
    /// underlay source address as the IBRL tunnel that took the last endpoint. It must fail
    /// instead — the mock has no `create_user` expectation, so a create here panics the test.
    #[test]
    fn test_connect_bare_with_device_rejects_a_starved_endpoint_instead_of_writing_unspecified() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (group_pk, _) = fixture.add_multicast_group("test-group", "239.0.0.1");
            fixture.accesspass.lock().unwrap().mgroup_sub_allowlist = vec![group_pk];

            // One device, and this machine's IBRL user already holds its only endpoint.
            let (device_pk, device) = fixture.add_device(DeviceType::Hybrid, 100, true);
            let ibrl_user = fixture.create_user(UserType::IBRL, device_pk, "1.2.3.4");
            assert_eq!(
                ibrl_user.tunnel_endpoint, device.public_ip,
                "the fixture must leave the device with no free endpoint"
            );
            fixture.add_user(&ibrl_user);

            let command = Connect {
                device: Some(device.code.clone()),
                ..bare_connect("1.2.3.4")
            };
            let (result, output) = run(&fixture, command).await;

            assert!(result.is_err(), "{output}");
            // The IBRL leg is a no-op re-run: its user already exists.
            assert!(output.contains("IBRL: provisioned"), "{output}");
            assert!(output.contains("❌  Multicast failed"), "{output}");
            assert!(output.contains("no tunnel endpoint is free"), "{output}");
            assert!(!output.contains("✅  User Provisioned"), "{output}");
        });
    }

    /// `--allocate-addr` and `--tenant` on the bare form reach the IBRL leg.
    #[test]
    fn test_connect_bare_passes_tenant_and_allocate_addr_to_the_ibrl_leg() {
        block_on(async {
            let mut fixture = TestFixture::new();
            let (tenant_pk, tenant) = fixture.add_tenant("my-tenant");
            let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);

            let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
            fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

            let command = Connect {
                tenant: Some(tenant.code.clone()),
                allocate_addr: true,
                ..bare_connect("1.2.3.4")
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok(), "{:?}", result.err());
            assert!(
                output.contains("Using tenant 'my-tenant' from CLI argument."),
                "{output}"
            );
            assert!(output.contains("IBRL: provisioned"), "{output}");
        });
    }

    /// The bare-only flags must not be silently ignored when a subcommand is given.
    #[test]
    fn test_connect_bare_flags_rejected_with_a_subcommand() {
        let command = Connect {
            dz_mode: Some(DzMode::IBRL {
                tenant: None,
                allocate_addr: false,
            }),
            tenant: Some("my-tenant".to_string()),
            ..Default::default()
        };
        let Err(err) = command.parse_dz_mode() else {
            panic!("--tenant belongs to the bare form and must be rejected here");
        };
        assert!(
            format!("{err:#}").contains("apply to the bare 'doublezero connect'"),
            "{err:#}"
        );
    }
}
