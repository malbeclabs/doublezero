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
use doublezero_sdk::{
    commands::{
        multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
        user::{create::CreateUserCommand, create_subscribe::CreateSubscribeUserCommand},
    },
    Device, User, UserCYOA, UserStatus, UserType,
};
use doublezero_serviceability::state::accesspass::AccessPass;
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;

use crate::{
    client::{DaemonClient, StatusResponse},
    helpers::{init_spinner, resolve_client_ip},
    latency::{best_latency, retrieve_latencies, select_tunnel_endpoint},
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
    },
}

/// Connect your server to a doublezero device
#[derive(Args, Debug)]
pub struct Connect {
    #[clap(subcommand)]
    pub dz_mode: DzMode,

    /// [deprecated] Client IP address — ignored; set --client-ip on the daemon (doublezerod) instead
    #[arg(long, global = true)]
    pub client_ip: Option<String>,

    /// Device Pubkey or code to associate with the user
    #[arg(long, global = true)]
    pub device: Option<String>,

    /// Verbose output
    #[arg(short, long, global = true, default_value_t = false)]
    pub verbose: bool,
}

enum ParsedDzMode {
    Ibrl(UserType, Option<String>),
    Multicast {
        pub_groups: Vec<String>,
        sub_groups: Vec<String>,
    },
}

/// Why the AccessPass pre-flight rejected (or accepted) this connection. The
/// failure variants carry the reason so the caller renders a precise diagnostic
/// without re-reading the ledger.
#[derive(Debug, PartialEq)]
enum AccessPassCheck {
    /// A pass covers this connection.
    Valid,
    /// No pass at the exact-IP PDA nor at the dynamic (`0.0.0.0`) PDA.
    NotFound,
    /// A pass exists for `(client_ip, payer)` but its epoch does not cover a
    /// unicast connect. `last_access_epoch == 0` means the pass was never
    /// epoch-funded, which onchain enables multicast only.
    EpochExpired {
        last_access_epoch: u64,
        current_epoch: u64,
    },
}

/// AccessPass pre-flight. Returns the rejection reason instead of a bare bool
/// so the caller can explain the failure (see [`troubleshoot_accesspass`]).
/// With `enforce_epoch`, the pass must also cover the current epoch.
///
/// Mirrors `check_accesspass` in `smartcontract/cli/src/requirements.rs` —
/// keep the two in sync if AccessPass validity semantics change.
fn check_accesspass<L: LedgerClient>(
    ledger: &L,
    client_ip: Ipv4Addr,
    enforce_epoch: bool,
) -> eyre::Result<AccessPassCheck> {
    let Some(accesspass) = ledger.get_accesspass(client_ip, ledger.get_payer())? else {
        return Ok(AccessPassCheck::NotFound);
    };

    if !enforce_epoch {
        return Ok(AccessPassCheck::Valid);
    }
    let current_epoch = ledger.get_epoch()?;
    // Mirrors epoch_allows_connection() onchain: unicast requires
    // last_access_epoch >= current_epoch, so 0 blocks every unicast type.
    if accesspass.last_access_epoch >= current_epoch {
        return Ok(AccessPassCheck::Valid);
    }
    Ok(AccessPassCheck::EpochExpired {
        last_access_epoch: accesspass.last_access_epoch,
        current_epoch,
    })
}

/// At most this many candidate passes are listed before the rest are summarized
/// as a count — a shared egress IP can legitimately carry many passes.
const MAX_TROUBLESHOOT_CANDIDATES: usize = 10;

/// Diagnostic run when no valid AccessPass authorizes this connection.
///
/// For [`AccessPassCheck::EpochExpired`] the offending pass is already known, so
/// the epoch is explained directly and no ledger scan happens. For
/// [`AccessPassCheck::NotFound`] every AccessPass on the ledger is scanned for
/// ones referencing the operator's current IP or DoubleZero ID (payer) and each
/// candidate is explained.
///
/// A pass authorizes `(client_ip, payer)` only when its `user_payer` is the
/// payer **and** it lives at one of the two PDAs the connect path derives:
/// `(client_ip, payer)` or the dynamic `(0.0.0.0, payer)` seat — see
/// `create_core.rs` and `GetAccessPassCommand`. Note `allow_multiple_ip` does
/// *not* widen that lookup: it only relaxes the stored-`client_ip` comparison
/// for a pass already found at a valid PDA, so a pass sitting at another IP's
/// PDA stays unreachable with the flag set.
///
/// Purely informational: any ledger read error is reported as an unavailable
/// troubleshooting note rather than propagated, so the caller's original
/// "no valid AccessPass" error remains the outcome.
fn troubleshoot_accesspass<L: LedgerClient, W: Write>(
    ledger: &L,
    client_ip: Ipv4Addr,
    payer: Pubkey,
    connecting_unicast: bool,
    failure: &AccessPassCheck,
    out: &mut W,
) -> eyre::Result<()> {
    match failure {
        AccessPassCheck::Valid => return Ok(()),
        // The pass that governs this connection was found; the epoch is the
        // whole story, so explain it instead of scanning the ledger.
        AccessPassCheck::EpochExpired {
            last_access_epoch: 0,
            ..
        } => {
            writeln!(
                out,
                "🔍  Your AccessPass ({client_ip} / {payer}) has no epochs funded (last_access_epoch = 0)."
            )?;
            writeln!(
                out,
                "    An unfunded pass enables multicast only; a unicast (IBRL) connect needs epochs on the pass."
            )?;
            return Ok(());
        }
        AccessPassCheck::EpochExpired {
            last_access_epoch,
            current_epoch,
        } => {
            writeln!(
                out,
                "🔍  Your AccessPass ({client_ip} / {payer}) expired: it covers up to epoch {last_access_epoch}, current epoch is {current_epoch}."
            )?;
            writeln!(
                out,
                "    It must be extended before a unicast (IBRL) connect will be admitted."
            )?;
            return Ok(());
        }
        AccessPassCheck::NotFound => {}
    }

    let passes = match ledger.list_accesspass() {
        Ok(passes) => passes,
        Err(err) => {
            writeln!(
                out,
                "🔍  Troubleshooting unavailable: could not list AccessPasses ({err})"
            )?;
            return Ok(());
        }
    };
    // Best-effort: without it, epoch coverage is reported as unknown rather than
    // turning the diagnostic into a hard failure.
    let current_epoch = ledger.get_epoch().ok();

    // Candidates: any pass referencing our current IP or our DoubleZero ID. A
    // dynamic 0.0.0.0 seat bound to our payer is matched via the payer check.
    // Matching on an unspecified client IP would sweep in every dynamic seat on
    // the ledger, so fall back to the payer alone in that case.
    let ip_is_specific = client_ip != Ipv4Addr::UNSPECIFIED;
    let mut candidates: Vec<(&Pubkey, &AccessPass)> = passes
        .iter()
        .filter(|(_, ap)| ap.user_payer == payer || (ip_is_specific && ap.client_ip == client_ip))
        .collect();

    if candidates.is_empty() {
        writeln!(
            out,
            "🔍  No existing AccessPass references your IP ({client_ip}) or your DoubleZero ID ({payer})."
        )?;
        writeln!(
            out,
            "    A new AccessPass must be issued for this IP + DoubleZero ID (or a dynamic 0.0.0.0 seat for the ID)."
        )?;
        return Ok(());
    }

    // Closest matches first: same DoubleZero ID and IP, then same ID, then same
    // IP. The pubkey tiebreak keeps the order stable across HashMap iterations.
    candidates.sort_by_key(|(pubkey, ap)| {
        let rank = match (ap.user_payer == payer, ap.client_ip == client_ip) {
            (true, true) => 0,
            (true, false) => 1,
            (false, true) => 2,
            (false, false) => 3,
        };
        (rank, **pubkey)
    });

    let total = candidates.len();
    writeln!(
        out,
        "🔍  Found {total} existing AccessPass(es) referencing your IP or DoubleZero ID, but none authorizes this connection:"
    )?;

    for (pubkey, ap) in candidates.iter().take(MAX_TROUBLESHOOT_CANDIDATES) {
        writeln!(
            out,
            "    • {pubkey}  {} [{}]",
            ap.accesspass_type, ap.status
        )?;

        // Client IP. A 0.0.0.0 pass is the dynamic seat: valid for any IP.
        let ip_ok = ap.client_ip == client_ip || ap.client_ip == Ipv4Addr::UNSPECIFIED;
        if ap.client_ip == Ipv4Addr::UNSPECIFIED {
            writeln!(
                out,
                "        client_ip:  0.0.0.0  ✅ dynamic seat, valid for any IP"
            )?;
        } else if ap.client_ip == client_ip {
            writeln!(
                out,
                "        client_ip:  {}  ✅ matches your IP",
                ap.client_ip
            )?;
        } else {
            writeln!(
                out,
                "        client_ip:  {}  ❌ does not match your IP ({client_ip})",
                ap.client_ip
            )?;
            if ap.allow_multiple_ip() {
                writeln!(
                    out,
                    "                    (allow_multiple_ip is set, but a pass is only reachable at its own IP or as a 0.0.0.0 dynamic seat)"
                )?;
            }
        }

        // DoubleZero ID (payer).
        let payer_ok = ap.user_payer == payer;
        if payer_ok {
            writeln!(
                out,
                "        user_payer: {}  ✅ matches your DoubleZero ID",
                ap.user_payer
            )?;
        } else {
            writeln!(
                out,
                "        user_payer: {}  ❌ registered to a different DoubleZero ID (yours: {payer})",
                ap.user_payer
            )?;
        }

        // Epoch coverage. Unicast needs last_access_epoch >= current_epoch;
        // 0 means no epochs funded (multicast only). Multicast is not gated.
        let unicast_ok = match (ap.last_access_epoch, current_epoch) {
            (0, _) => {
                writeln!(
                    out,
                    "        epoch:      0  ❌ no epochs funded (multicast only)"
                )?;
                false
            }
            (u64::MAX, _) => {
                writeln!(out, "        epoch:      unlimited  ✅")?;
                true
            }
            (last, Some(current)) if last < current => {
                writeln!(
                    out,
                    "        epoch:      {last}  ❌ expired (current epoch {current})"
                )?;
                false
            }
            (last, Some(current)) => {
                writeln!(
                    out,
                    "        epoch:      {last}  ✅ (current epoch {current})"
                )?;
                true
            }
            (last, None) => {
                writeln!(out, "        epoch:      {last}  (current epoch unknown)")?;
                true
            }
        };

        // Concise summary of what disqualifies this pass.
        let mut problems = Vec::new();
        if !payer_ok {
            problems.push("wrong DoubleZero ID".to_string());
        }
        if !ip_ok {
            problems.push("not reachable from your IP".to_string());
        }
        if connecting_unicast && !unicast_ok {
            problems.push(if ap.last_access_epoch == 0 {
                "no epochs funded (unicast needs them)".to_string()
            } else {
                format!("expired at epoch {}", ap.last_access_epoch)
            });
        }
        if problems.is_empty() {
            // Every check passed, yet the pre-flight found no pass at either
            // derivable PDA — the stored client_ip disagrees with the PDA seed
            // it lives at. Say so rather than leaving the failure unexplained.
            writeln!(
                out,
                "        → all checks pass, yet the connect path cannot derive this pass — please report this address to support"
            )?;
        } else {
            writeln!(out, "        → not usable: {}", problems.join(", "))?;
        }
    }

    if total > MAX_TROUBLESHOOT_CANDIDATES {
        writeln!(
            out,
            "    … and {} more (use `doublezero accesspass list` to see all)",
            total - MAX_TROUBLESHOOT_CANDIDATES
        )?;
    }

    Ok(())
}

impl Connect {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        let spinner = init_spinner(5);

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

        let parsed_mode = self.parse_dz_mode()?;
        // Multicast users are not subject to epoch expiry — only verify the AccessPass exists.
        let enforce_epoch = !matches!(parsed_mode, ParsedDzMode::Multicast { .. });

        let accesspass_check = check_accesspass(ledger, client_ip, enforce_epoch)?;
        if accesspass_check != AccessPassCheck::Valid {
            writeln!(
                out,
                "❌  Unable to find a valid AccessPass for the IP: {client_ip_str} UserPayer: {}",
                ledger.get_payer()
            )?;
            // enforce_epoch is true exactly for unicast (IBRL) connects.
            troubleshoot_accesspass(
                ledger,
                client_ip,
                ledger.get_payer(),
                enforce_epoch,
                &accesspass_check,
                out,
            )?;
            return Err(eyre::eyre!(
                "A valid AccessPass is required to connect. Please contact support to obtain one."
            ));
        }

        spinner.inc(1);
        writeln!(out, "    DoubleZero ID: {}", ledger.get_payer())?;
        writeln!(out, "⚡  Provisioning for IP: {client_ip_str}")?;

        let provisioned = match parsed_mode {
            ParsedDzMode::Ibrl(user_type, tenant) => {
                self.execute_ibrl(ledger, daemon, user_type, client_ip, tenant, &spinner, out)
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

    #[allow(clippy::too_many_arguments)]
    async fn execute_ibrl<D: DaemonClient, L: LedgerClient, W: Write>(
        &self,
        ledger: &L,
        daemon: &D,
        user_type: UserType,
        client_ip: Ipv4Addr,
        tenant: Option<String>,
        spinner: &ProgressBar,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Look for user
        let (_user_pubkey, user) = self
            .find_or_create_user(ledger, daemon, &client_ip, spinner, user_type, tenant, out)
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

    fn parse_dz_mode(&self) -> eyre::Result<ParsedDzMode> {
        match &self.dz_mode {
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
            } => {
                let has_legacy = mode.is_some() || !multicast_groups.is_empty();
                let has_new = !pub_groups.is_empty() || !sub_groups.is_empty();

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

                // Create user with first group (pick from pub_groups first, then sub_groups)
                let first_group_pk = all_group_pks
                    .first()
                    .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

                let res = ledger.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: ibrl_user.cyoa_type,
                    client_ip: *client_ip,
                    mgroup_pk: *first_group_pk,
                    publisher: pub_group_pks.contains(first_group_pk),
                    subscriber: sub_group_pks.contains(first_group_pk),
                    tunnel_endpoint,
                    owner: None,
                    feed_pk: None,
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

                // Wait for user to be activated before subscribing to additional groups
                if all_group_pks.len() > 1 {
                    self.poll_for_user_activated(ledger, &user_pk, spinner)?;
                }

                // Subscribe to remaining groups
                for group_pk in all_group_pks.iter().skip(1) {
                    spinner.set_message(format!("Subscribing to group: {group_pk}"));
                    ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                        user_pk,
                        group_pk: *group_pk,
                        client_ip: *client_ip,
                        publisher: pub_group_pks.contains(group_pk),
                        subscriber: sub_group_pks.contains(group_pk),
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

                // Subscribe to any pub groups not already subscribed
                for group_pk in pub_group_pks {
                    if !user.publishers.contains(group_pk) {
                        spinner.set_message(format!(
                            "Adding publisher subscription to existing Multicast user: {user_pk}"
                        ));

                        let res =
                            ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                                user_pk: *user_pk,
                                group_pk: *group_pk,
                                client_ip: *client_ip,
                                publisher: true,
                                subscriber: false,
                                device_pk: None,
                                feed_pk: None,
                            });

                        match res {
                            Ok(_) => {
                                spinner.set_message("Publisher subscription added");
                            }
                            Err(e) => {
                                writeln!(out, "❌ Error adding publisher subscription")?;
                                writeln!(out, "\nError: {e:?}\n")?;
                                eyre::bail!(
                                    "Error adding publisher subscription to existing user: {e:?}"
                                );
                            }
                        }
                    }
                }

                // Subscribe to any sub groups not already subscribed
                for group_pk in sub_group_pks {
                    if !user.subscribers.contains(group_pk) {
                        spinner.set_message(format!(
                            "Adding subscriber subscription to existing Multicast user: {user_pk}"
                        ));

                        let res =
                            ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                                user_pk: *user_pk,
                                group_pk: *group_pk,
                                client_ip: *client_ip,
                                publisher: false,
                                subscriber: true,
                                device_pk: None,
                                feed_pk: None,
                            });

                        match res {
                            Ok(_) => {
                                spinner.set_message("Subscriber subscription added");
                            }
                            Err(e) => {
                                writeln!(out, "❌ Error adding subscriber subscription")?;
                                writeln!(out, "\nError: {e:?}\n")?;
                                eyre::bail!(
                                    "Error adding subscriber subscription to existing user: {e:?}"
                                );
                            }
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

                // Create user with first group (pick from pub_groups first, then sub_groups)
                let first_group_pk = all_group_pks
                    .first()
                    .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

                let res = ledger.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    mgroup_pk: *first_group_pk,
                    publisher: pub_group_pks.contains(first_group_pk),
                    subscriber: sub_group_pks.contains(first_group_pk),
                    tunnel_endpoint,
                    owner: None,
                    feed_pk: None,
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

                // Wait for user to be activated before subscribing to additional groups
                if all_group_pks.len() > 1 {
                    self.poll_for_user_activated(ledger, &user_pk, spinner)?;
                }

                // Subscribe to remaining groups
                for group_pk in all_group_pks.iter().skip(1) {
                    spinner.set_message(format!("Subscribing to group: {group_pk}"));
                    ledger.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                        user_pk,
                        group_pk: *group_pk,
                        client_ip: *client_ip,
                        publisher: pub_group_pks.contains(group_pk),
                        subscriber: sub_group_pks.contains(group_pk),
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        client::{
            DoubleZeroStatus, LatencyRecord, LatencyResponse, MockDaemonClient, StatusResponse,
            V2StatusResponse,
        },
        ledger::MockLedgerClient,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_config::Environment;
    use doublezero_sdk::{tests::utils::create_temp_config, utils::parse_pubkey};
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::{Device, DeviceStatus, DeviceType},
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

    struct TestFixture {
        pub ledger: MockLedgerClient,
        pub daemon: MockDaemonClient,
        pub devices: Arc<Mutex<HashMap<Pubkey, Device>>>,
        pub users: Arc<Mutex<HashMap<Pubkey, User>>>,
        pub latencies: Arc<Mutex<Vec<LatencyRecord>>>,
        pub mcast_groups: Arc<Mutex<HashMap<Pubkey, MulticastGroup>>>,
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
                users: Arc::new(Mutex::new(HashMap::new())),
                latencies: Arc::new(Mutex::new(vec![])),
                mcast_groups: Arc::new(Mutex::new(HashMap::new())),
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

            let accesspass = fixture.accesspass.clone();
            fixture
                .ledger
                .expect_list_accesspass()
                .returning_st(move || {
                    let ap = accesspass.lock().unwrap().clone();
                    Ok(HashMap::from([(Pubkey::new_unique(), ap)]))
                });

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

            let mcast_groups = fixture.mcast_groups.clone();
            fixture
                .ledger
                .expect_list_multicastgroup()
                .returning_st(move || Ok(mcast_groups.lock().unwrap().clone()));

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
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: device_number as u128,
                bump_seed: 255,
                reference_count: 0,
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
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
            let expected_create_user_command = CreateUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                tunnel_endpoint: user.tunnel_endpoint,
                tenant_pk,
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
            mcast_group_pk: Pubkey,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_create_subscribe_user_command = CreateSubscribeUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                mgroup_pk: mcast_group_pk,
                publisher,
                subscriber,
                tunnel_endpoint: user.tunnel_endpoint,
                owner: None,
                feed_pk: None,
            };

            let users = self.users.clone();
            let provisioned = self.provisioned_services.clone();
            let mut user = user.clone();
            if publisher {
                user.publishers.push(mcast_group_pk);
            }
            if subscriber {
                user.subscribers.push(mcast_group_pk);
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
            mcast_group_pk: Pubkey,
            client_ip: Ipv4Addr,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_command = UpdateMulticastGroupRolesCommand {
                user_pk,
                group_pk: mcast_group_pk,
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
                            user.publishers.push(cmd.group_pk);
                        }
                        if cmd.subscriber {
                            user.subscribers.push(cmd.group_pk);
                        }
                        provisioned
                            .lock()
                            .unwrap()
                            .insert(user.user_type.to_string());
                    }
                    Ok(())
                });
        }
    }

    /// Run `connect` against the fixture's mocks with a captured writer,
    /// returning the result and the writer output.
    async fn run(fixture: &TestFixture, command: Connect) -> (eyre::Result<()>, String) {
        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let result = command
            .execute(&ctx, &fixture.daemon, &fixture.ledger, &mut out)
            .await;
        (result, String::from_utf8(out).unwrap())
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
                dz_mode: DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                true,  // publisher
                false, // subscriber
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
            };

            let (result, output) = run(&fixture, command).await;
            assert!(result.is_ok());
            assert!(output.contains("Creating separate Multicast user for concurrent tunnels"));
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
                dz_mode: DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                true,  // publisher
                false, // subscriber
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: true,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: true,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: true,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                true,
                false,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
            fixture.expect_create_subscribe_user(user_pk, &user, g1_pk, true, true);
            // Remaining group (g2) added via update_multicastgroup_roles as subscriber-only.
            fixture.expect_update_multicastgroup_roles(
                user_pk,
                g2_pk,
                Ipv4Addr::new(1, 2, 3, 4),
                false,
                true,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: None,
                device: None,
                verbose: false,
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
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: None,
                device: None,
                verbose: false,
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
            fixture.expect_create_subscribe_user(user_pk, &user, g1_pk, false, true);

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: None,
                device: None,
                verbose: false,
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
            dz_mode: DzMode::Multicast {
                mode: None,
                multicast_groups: vec![],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: None,
            device: None,
            verbose: false,
        };

        match command.parse_dz_mode().unwrap() {
            ParsedDzMode::Multicast {
                pub_groups,
                sub_groups,
            } => {
                assert!(pub_groups.is_empty());
                assert!(sub_groups.is_empty());
            }
            ParsedDzMode::Ibrl(..) => panic!("expected ParsedDzMode::Multicast, got Ibrl"),
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
                mcast_group_pk,
                true,
                false,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec!["test-group".to_string()],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                false,
                true,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group".to_string()],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                false,
                true,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group".to_string()],
                },
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                verbose: false,
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
    /// which calls UpdateMulticastGroupRoles (the on-chain processor never had an epoch
    /// check; this test verifies the CLI gate no longer blocks it either).
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
                mcast_group2_pk,
                user.client_ip,
                false,
                true,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: None,
                    multicast_groups: vec![],
                    pub_groups: vec![],
                    sub_groups: vec!["test-group2".to_string()],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                verbose: false,
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
            // The failure path also runs AccessPass troubleshooting. The fixture
            // pass governs this IP + DoubleZero ID and has last_access_epoch = 0,
            // so the epoch is the whole story: no ledger scan, just the reason.
            assert!(
                output.contains("no epochs funded (last_access_epoch = 0)"),
                "expected epoch diagnostic, got: {output}"
            );
            assert!(output.contains("enables multicast only"), "{output}");
            assert!(
                !output.contains("existing AccessPass(es)"),
                "must not scan the ledger when the offending pass is known: {output}"
            );
        });
    }

    fn troubleshoot_pass(
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
        last_access_epoch: u64,
    ) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: user_payer,
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch,
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
        }
    }

    /// Ledger mock that serves `passes` and a current epoch of 100.
    fn troubleshoot_ledger(passes: Vec<(Pubkey, AccessPass)>) -> MockLedgerClient {
        let mut ledger = MockLedgerClient::new();
        let passes: HashMap<Pubkey, AccessPass> = passes.into_iter().collect();
        ledger
            .expect_list_accesspass()
            .returning_st(move || Ok(passes.clone()));
        ledger.expect_get_epoch().returning(|| Ok(100));
        ledger
    }

    fn run_troubleshoot(
        ledger: &MockLedgerClient,
        client_ip: Ipv4Addr,
        payer: Pubkey,
        connecting_unicast: bool,
    ) -> String {
        let mut out = Vec::new();
        troubleshoot_accesspass(
            ledger,
            client_ip,
            payer,
            connecting_unicast,
            &AccessPassCheck::NotFound,
            &mut out,
        )
        .unwrap();
        String::from_utf8(out).unwrap()
    }

    #[test]
    fn test_troubleshoot_accesspass_reports_mismatches() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let other_payer = Pubkey::new_unique();
        let unrelated_payer = Pubkey::new_unique();

        // Same IP, different DoubleZero ID.
        let wrong_payer = troubleshoot_pass(my_ip, other_payer, u64::MAX);
        // My DoubleZero ID, different IP.
        let wrong_ip = troubleshoot_pass(Ipv4Addr::new(9, 9, 9, 9), my_payer, u64::MAX);
        // My IP + DoubleZero ID, but no epochs funded: cannot serve unicast.
        let unfunded = troubleshoot_pass(my_ip, my_payer, 0);
        // My IP + DoubleZero ID, but the epoch is in the past.
        let expired = troubleshoot_pass(my_ip, my_payer, 42);
        // Unrelated — must be excluded from the candidates.
        let unrelated = troubleshoot_pass(Ipv4Addr::new(8, 8, 8, 8), unrelated_payer, u64::MAX);

        let ledger = troubleshoot_ledger(vec![
            (Pubkey::new_unique(), wrong_payer),
            (Pubkey::new_unique(), wrong_ip),
            (Pubkey::new_unique(), unfunded),
            (Pubkey::new_unique(), expired),
            (Pubkey::new_unique(), unrelated),
        ]);
        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);

        // Four of the five passes are candidates (the unrelated one is excluded).
        assert!(
            output.contains("Found 4 existing AccessPass(es)"),
            "{output}"
        );
        assert!(output.contains("different DoubleZero ID"), "{output}");
        assert!(output.contains("does not match your IP"), "{output}");
        assert!(
            output.contains("no epochs funded (multicast only)"),
            "{output}"
        );
        assert!(output.contains("expired (current epoch 100)"), "{output}");
        assert!(
            output.contains("→ not usable: expired at epoch 42"),
            "{output}"
        );
        // The unrelated pass's IP must not appear.
        assert!(!output.contains("8.8.8.8"), "{output}");
        // Every candidate carries a "not usable" verdict — no silent candidates.
        assert_eq!(
            output.matches("→ not usable:").count(),
            4,
            "every candidate must state why it is unusable: {output}"
        );
        assert!(
            !output.contains("all checks pass"),
            "no candidate here should look fully valid: {output}"
        );
    }

    /// Each candidate is printed with its account address so support can act on it.
    #[test]
    fn test_troubleshoot_accesspass_prints_pass_pubkey() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let pass_pk = Pubkey::new_unique();
        let ledger = troubleshoot_ledger(vec![(pass_pk, troubleshoot_pass(my_ip, my_payer, 0))]);

        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);
        assert!(output.contains(&pass_pk.to_string()), "{output}");
    }

    /// `allow_multiple_ip` does not make a pass at another IP's PDA reachable:
    /// it must be reported as unusable, with the flag's real scope explained.
    #[test]
    fn test_troubleshoot_accesspass_allow_multiple_ip_is_not_reachable() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let mut pass = troubleshoot_pass(Ipv4Addr::new(9, 9, 9, 9), my_payer, u64::MAX);
        pass.flags = doublezero_serviceability::state::accesspass::ALLOW_MULTIPLE_IP;

        let ledger = troubleshoot_ledger(vec![(Pubkey::new_unique(), pass)]);
        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);

        assert!(output.contains("does not match your IP"), "{output}");
        assert!(
            output.contains("→ not usable: not reachable from your IP"),
            "{output}"
        );
        assert!(output.contains("allow_multiple_ip is set, but"), "{output}");
    }

    /// A 0.0.0.0 dynamic seat is valid for any IP; only its epoch disqualifies it here.
    #[test]
    fn test_troubleshoot_accesspass_dynamic_seat() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let ledger = troubleshoot_ledger(vec![(
            Pubkey::new_unique(),
            troubleshoot_pass(Ipv4Addr::UNSPECIFIED, my_payer, 0),
        )]);

        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);
        assert!(
            output.contains("0.0.0.0  ✅ dynamic seat, valid for any IP"),
            "{output}"
        );
        assert!(
            output.contains("→ not usable: no epochs funded (unicast needs them)"),
            "{output}"
        );
    }

    /// An unspecified client IP must not sweep in every dynamic seat on the ledger.
    #[test]
    fn test_troubleshoot_accesspass_unspecified_client_ip_matches_payer_only() {
        let my_payer = Pubkey::new_unique();
        let mine = troubleshoot_pass(Ipv4Addr::UNSPECIFIED, my_payer, 0);
        let someone_elses = troubleshoot_pass(Ipv4Addr::UNSPECIFIED, Pubkey::new_unique(), 0);

        let ledger = troubleshoot_ledger(vec![
            (Pubkey::new_unique(), mine),
            (Pubkey::new_unique(), someone_elses),
        ]);
        let output = run_troubleshoot(&ledger, Ipv4Addr::UNSPECIFIED, my_payer, true);

        assert!(
            output.contains("Found 1 existing AccessPass(es)"),
            "only the payer's own dynamic seat is a candidate: {output}"
        );
    }

    /// Multicast is not epoch-gated, so an unfunded pass is not flagged for it.
    #[test]
    fn test_troubleshoot_accesspass_multicast_connect_ignores_epoch() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let ledger = troubleshoot_ledger(vec![(
            Pubkey::new_unique(),
            troubleshoot_pass(Ipv4Addr::new(9, 9, 9, 9), my_payer, 0),
        )]);

        let output = run_troubleshoot(&ledger, my_ip, my_payer, false);
        assert!(
            output.contains("→ not usable: not reachable from your IP"),
            "{output}"
        );
        assert!(
            !output.contains("unicast needs them"),
            "a multicast connect must not complain about epochs: {output}"
        );
    }

    /// The candidate list is capped, with the remainder summarized as a count.
    #[test]
    fn test_troubleshoot_accesspass_caps_candidate_list() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let passes: Vec<(Pubkey, AccessPass)> = (0..MAX_TROUBLESHOOT_CANDIDATES + 3)
            .map(|_| (Pubkey::new_unique(), troubleshoot_pass(my_ip, my_payer, 0)))
            .collect();

        let ledger = troubleshoot_ledger(passes);
        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);

        assert!(
            output.contains(&format!(
                "Found {} existing AccessPass(es)",
                MAX_TROUBLESHOOT_CANDIDATES + 3
            )),
            "{output}"
        );
        assert_eq!(
            output.matches("→ not usable:").count(),
            MAX_TROUBLESHOOT_CANDIDATES,
            "{output}"
        );
        assert!(output.contains("… and 3 more"), "{output}");
    }

    #[test]
    fn test_troubleshoot_accesspass_no_candidates() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let ledger = troubleshoot_ledger(vec![(
            Pubkey::new_unique(),
            troubleshoot_pass(Ipv4Addr::new(8, 8, 8, 8), Pubkey::new_unique(), u64::MAX),
        )]);

        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);
        assert!(
            output.contains("No existing AccessPass references your IP"),
            "{output}"
        );
        assert!(
            output.contains("A new AccessPass must be issued"),
            "{output}"
        );
    }

    /// A ledger read failure degrades to a note; it never replaces the caller's error.
    #[test]
    fn test_troubleshoot_accesspass_list_error_is_reported_not_propagated() {
        let mut ledger = MockLedgerClient::new();
        ledger
            .expect_list_accesspass()
            .returning_st(|| Err(eyre::eyre!("rpc down")));

        let output = run_troubleshoot(
            &ledger,
            Ipv4Addr::new(1, 2, 3, 4),
            Pubkey::new_unique(),
            true,
        );
        assert!(output.contains("Troubleshooting unavailable"), "{output}");
        assert!(output.contains("rpc down"), "{output}");
    }

    /// An unknown current epoch must not turn the diagnostic into a hard failure.
    #[test]
    fn test_troubleshoot_accesspass_tolerates_unknown_epoch() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        let pass = troubleshoot_pass(Ipv4Addr::new(9, 9, 9, 9), my_payer, 42);

        let mut ledger = MockLedgerClient::new();
        ledger
            .expect_list_accesspass()
            .returning_st(move || Ok(HashMap::from([(Pubkey::new_unique(), pass.clone())])));
        ledger
            .expect_get_epoch()
            .returning(|| Err(eyre::eyre!("rpc down")));

        let output = run_troubleshoot(&ledger, my_ip, my_payer, true);
        assert!(output.contains("current epoch unknown"), "{output}");
        assert!(
            output.contains("→ not usable: not reachable from your IP"),
            "{output}"
        );
    }

    /// The epoch failure path names the epochs and skips the ledger scan entirely.
    #[test]
    fn test_troubleshoot_accesspass_expired_epoch_is_explained() {
        let my_ip = Ipv4Addr::new(1, 2, 3, 4);
        let my_payer = Pubkey::new_unique();
        // No list_accesspass expectation: reaching the scan would panic the mock.
        let ledger = MockLedgerClient::new();

        let mut out = Vec::new();
        troubleshoot_accesspass(
            &ledger,
            my_ip,
            my_payer,
            true,
            &AccessPassCheck::EpochExpired {
                last_access_epoch: 42,
                current_epoch: 100,
            },
            &mut out,
        )
        .unwrap();
        let output = String::from_utf8(out).unwrap();

        assert!(
            output.contains("covers up to epoch 42, current epoch is 100"),
            "{output}"
        );
        assert!(output.contains("must be extended"), "{output}");
    }

    /// check_accesspass reports *why* it rejected, so the caller can explain it.
    #[test]
    fn test_check_accesspass_outcomes() {
        let client_ip = Ipv4Addr::new(1, 2, 3, 4);

        // Missing pass.
        let mut ledger = MockLedgerClient::new();
        ledger.expect_get_payer().returning(Pubkey::new_unique);
        ledger.expect_get_accesspass().returning(|_, _| Ok(None));
        assert_eq!(
            check_accesspass(&ledger, client_ip, true).unwrap(),
            AccessPassCheck::NotFound
        );

        // Stale epoch: rejected for unicast, accepted for multicast.
        let mut ledger = MockLedgerClient::new();
        ledger.expect_get_payer().returning(Pubkey::new_unique);
        ledger
            .expect_get_accesspass()
            .returning(move |ip, payer| Ok(Some(troubleshoot_pass(ip, payer, 42))));
        ledger.expect_get_epoch().returning(|| Ok(100));
        assert_eq!(
            check_accesspass(&ledger, client_ip, true).unwrap(),
            AccessPassCheck::EpochExpired {
                last_access_epoch: 42,
                current_epoch: 100,
            }
        );
        assert_eq!(
            check_accesspass(&ledger, client_ip, false).unwrap(),
            AccessPassCheck::Valid
        );

        // Epoch exactly at the boundary is still valid.
        let mut ledger = MockLedgerClient::new();
        ledger.expect_get_payer().returning(Pubkey::new_unique);
        ledger
            .expect_get_accesspass()
            .returning(move |ip, payer| Ok(Some(troubleshoot_pass(ip, payer, 100))));
        ledger.expect_get_epoch().returning(|| Ok(100));
        assert_eq!(
            check_accesspass(&ledger, client_ip, true).unwrap(),
            AccessPassCheck::Valid
        );
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
            mcast_group2_pk,
            user.client_ip,
            publisher,
            subscriber,
        );

        let command = Connect {
            dz_mode: DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group2".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
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
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    // Pass the same group twice — should error
                    multicast_groups: vec!["test-group".to_string(), "test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group2_pk,
                user.client_ip,
                false,
                true,
            );

            // Add subscriber group to existing publisher - should succeed
            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group2".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group2_pk,
                user.client_ip,
                true,
                false,
            );

            // Add publisher group to existing subscriber - should succeed
            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group2".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
            dz_mode: DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
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
                mcast_group_pk,
                false,
                true,
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
            };

            let (result, _) = run(&fixture, command).await;
            assert!(result.is_ok());

            // Test that adding an IBRL tunnel with an existing multicast succeeds on a DIFFERENT device
            // (concurrent tunnels from same client IP must go to different devices)
            let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
            fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

            let command = Connect {
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                false,
                true,
            );

            // Save device latencies for delayed availability test
            let latency_record_device1 = fixture.latencies.lock().unwrap()[0].clone();
            let latency_record_device2 = fixture.latencies.lock().unwrap()[1].clone();
            fixture.latencies.lock().unwrap().clear();
            let latencies = Arc::clone(&fixture.latencies);

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: Some(device.code.clone()), // Explicitly specify the device
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: Some(device.code.clone()), // Explicitly specify the device
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: None, // auto-select
                verbose: false,
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
                mcast_group_pk,
                true,  // publisher
                false, // not subscriber
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None, // auto-select
                verbose: false,
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
                mcast_group_pk,
                false, // not publisher
                true,  // subscriber
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Publisher),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some("1.2.3.4".to_string()),
                device: Some("nonexistent-device".to_string()), // Device that doesn't exist
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: true,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                mcast_group_pk,
                false, // publisher
                true,  // subscriber
            );

            let command = Connect {
                dz_mode: DzMode::Multicast {
                    mode: Some(MulticastMode::Subscriber),
                    multicast_groups: vec!["test-group".to_string()],
                    pub_groups: vec![],
                    sub_groups: vec![],
                },
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some("test-tenant".to_string()),
                    allocate_addr: false,
                },
                client_ip: Some(ibrl_user.client_ip.to_string()),
                device: None,
                verbose: false,
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
                dz_mode: DzMode::IBRL {
                    tenant: Some(tenant.code.clone()),
                    allocate_addr: false,
                },
                client_ip: Some(user.client_ip.to_string()),
                device: None,
                verbose: false,
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
}
