//! Diffs an access-pass definition document against the ledger, and renders the result.
//!
//! Produces the allowlist grants and revokes, and the IBRL (tenant) changes, that would bring the
//! ledger to the state the document describes — plus the declared grants already satisfied, the
//! entries it refuses to act on, and any warnings. `plan` prints that; `apply` prints it and then
//! sends it.

use crate::{
    accesspass::desired::{AccessPassDocument, DesiredAccessPass},
    doublezerocommand::CliCommand,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, feed::list::ListFeedCommand,
    multicastgroup::list::ListMulticastGroupCommand, tenant::list::ListTenantCommand,
};
use doublezero_serviceability::state::accesspass::AccessPass;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::{BTreeSet, HashMap},
    io::Write,
    net::Ipv4Addr,
    path::PathBuf,
};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    Publisher,
    Subscriber,
    /// Unicast access, granted by the pass's tenant rather than a multicast allowlist.
    Ibrl,
}

impl Role {
    pub fn label(self) -> &'static str {
        match self {
            Role::Publisher => "publisher",
            Role::Subscriber => "subscriber",
            Role::Ibrl => "ibrl",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Op {
    Grant,
    Revoke,
}

/// One allowlist write the plan would make.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct PlannedChange {
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "ser_pubkey")]
    pub user_payer: Pubkey,
    #[serde(serialize_with = "ser_pubkey")]
    pub access_pass: Pubkey,
    pub group: String,
    pub role: Role,
    pub op: Op,
}

/// A change to the pass's IBRL (unicast) grant: its tenant, and the epoch that gates it.
///
/// The tenant and `last_access_epoch` are one grant, written by the same `access-pass set`. A
/// declared `ibrl` means the epoch must be unlimited: the epoch gates unicast user creation only,
/// and any finite value turns a later `connect ibrl` on this IP into a failure at an unpredictable
/// date. Either half drifting re-sends the same instruction.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct IbrlChange {
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "ser_pubkey")]
    pub user_payer: Pubkey,
    #[serde(serialize_with = "ser_pubkey")]
    pub access_pass: Pubkey,
    /// Tenant code currently on the pass, if any.
    pub from: Option<String>,
    /// Tenant code the document declares, or `None` to clear the grant.
    pub to: Option<String>,
    /// `to` resolved to its account, or the default pubkey to clear. Skipped in JSON: the code is
    /// what a reader wants, and the key is an implementation detail of the write.
    #[serde(skip)]
    pub to_pk: Pubkey,
    /// The epoch is not already unlimited, so this write moves it too.
    pub epoch_drift: bool,
}

/// A declared grant that is already satisfied, and by what.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct SatisfiedGrant {
    pub client_ip: Ipv4Addr,
    pub group: String,
    pub role: Role,
    /// `allowlist`, or `feed <code>` when an EdgeSeat feed already grants it.
    pub source: String,
}

/// Something the plan refuses to do, and why.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct BlockedItem {
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "ser_pubkey")]
    pub user_payer: Pubkey,
    pub reason: String,
}

#[derive(Debug, Default, Clone, PartialEq, Serialize)]
pub struct AccessPassPlan {
    pub changes: Vec<PlannedChange>,
    pub ibrl_changes: Vec<IbrlChange>,
    pub satisfied: Vec<SatisfiedGrant>,
    pub blocked: Vec<BlockedItem>,
    pub warnings: Vec<String>,
}

fn ser_pubkey<S: serde::Serializer>(pk: &Pubkey, s: S) -> Result<S::Ok, S::Error> {
    s.serialize_str(&pk.to_string())
}

impl AccessPassPlan {
    pub fn grants(&self) -> usize {
        self.changes.iter().filter(|c| c.op == Op::Grant).count()
    }

    pub fn revokes(&self) -> usize {
        self.changes.iter().filter(|c| c.op == Op::Revoke).count()
    }

    pub fn is_empty(&self) -> bool {
        self.changes.is_empty() && self.ibrl_changes.is_empty()
    }

    /// Total writes the plan would make, across both instruction kinds.
    pub fn write_count(&self) -> usize {
        self.changes.len() + self.ibrl_changes.len()
    }
}

/// Tenant codes in both directions, read once per run.
struct TenantCodes {
    by_code: HashMap<String, Pubkey>,
    by_pubkey: HashMap<Pubkey, String>,
}

impl TenantCodes {
    fn read<C: CliCommand>(client: &C) -> eyre::Result<Self> {
        let tenants = client.list_tenant(ListTenantCommand {})?;
        Ok(Self {
            by_code: tenants
                .iter()
                .map(|(pk, t)| (t.code.clone(), *pk))
                .collect(),
            by_pubkey: tenants
                .iter()
                .map(|(pk, t)| (*pk, t.code.clone()))
                .collect(),
        })
    }
}

/// Builds the diff between the document and the ledger.
///
/// Reads are batched where the account model allows: the multicast groups and the feeds are each
/// one scan for the whole document, and only the access passes are fetched per entry, because
/// each is a distinct PDA.
pub fn build_plan<C: CliCommand>(
    client: &C,
    desired: &[DesiredAccessPass],
) -> eyre::Result<AccessPassPlan> {
    let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
    let code_to_pk: HashMap<&str, Pubkey> = mgroups
        .iter()
        .map(|(pk, mg)| (mg.code.as_str(), *pk))
        .collect();
    let pk_to_code: HashMap<Pubkey, &str> = mgroups
        .iter()
        .map(|(pk, mg)| (*pk, mg.code.as_str()))
        .collect();

    // A code the program cannot resolve becomes a grant pointing at nothing rather than an error,
    // so every code in the document is checked before anything is planned. Report all of them at
    // once: fixing a typo only to hit the next one is a poor way to spend a round trip.
    let unknown: BTreeSet<&str> = desired
        .iter()
        .flat_map(|d| d.publish.iter().chain(d.subscribe.iter()))
        .map(String::as_str)
        .filter(|code| !code_to_pk.contains_key(code))
        .collect();
    if !unknown.is_empty() {
        eyre::bail!(
            "unknown multicast group code(s): {}",
            unknown.into_iter().collect::<Vec<_>>().join(", ")
        );
    }

    // Tenants are read when the document declares an `ibrl`, and otherwise only once a pass turns
    // out to carry a tenant — a pass with one still has to be cleared, so the scan cannot be
    // skipped merely because the document is silent. `None` means "not read yet".
    let mut tenants: Option<TenantCodes> = None;
    if desired.iter().any(|d| d.ibrl.is_some()) {
        let read = TenantCodes::read(client)?;
        // Codes are checked for the same reason group codes are: the program resolves a code to a
        // PDA without checking anything is behind it, so a typo writes a grant pointing at nothing.
        let unknown: BTreeSet<&str> = desired
            .iter()
            .filter_map(|d| d.ibrl.as_deref())
            .filter(|code| !read.by_code.contains_key(*code))
            .collect();
        if !unknown.is_empty() {
            eyre::bail!(
                "unknown tenant code(s): {}",
                unknown.into_iter().collect::<Vec<_>>().join(", ")
            );
        }
        tenants = Some(read);
    }

    let mut plan = AccessPassPlan::default();
    let mut feeds: Option<HashMap<Pubkey, doublezero_sdk::Feed>> = None;

    for entry in desired {
        let Some((pass_pk, pass)) = client.get_accesspass(GetAccessPassCommand {
            client_ip: entry.client_ip,
            user_payer: entry.user_payer,
        })?
        else {
            plan.blocked.push(BlockedItem {
                client_ip: entry.client_ip,
                user_payer: entry.user_payer,
                reason: "no access pass at this PDA — create it with `doublezero access-pass set`"
                    .to_string(),
            });
            continue;
        };

        // Access-pass resolution prefers a pass stored at the 0.0.0.0 PDA, which is valid for any
        // client IP. So a lookup for a concrete address can legitimately land on a shared pass —
        // and every group granted here is then granted to every host using that pass.
        if pass.client_ip != entry.client_ip {
            plan.warnings.push(format!(
                "{} resolved to the shared access pass at {} ({pass_pk}); \
                 changes here affect every host using it",
                entry.client_ip, pass.client_ip
            ));
        }

        // Only an EdgeSeat pass carries feeds, so a document of ordinary passes never pays for
        // the scan.
        if feeds.is_none() && !pass.feed_seats().is_empty() {
            feeds = Some(client.list_feed(ListFeedCommand)?);
        }
        let feed_granted = feed_granted_groups(&pass, feeds.as_ref(), &pk_to_code);

        let have_pub = allowlist_codes(&pass.mgroup_pub_allowlist, &pk_to_code);
        let have_sub = allowlist_codes(&pass.mgroup_sub_allowlist, &pk_to_code);
        let want_pub: BTreeSet<&str> = entry.publish.iter().map(String::as_str).collect();
        let want_sub: BTreeSet<&str> = entry.subscribe.iter().map(String::as_str).collect();

        // A group leaving both allowlists at once cannot be revoked safely. The host's detach
        // verbs send the role being KEPT as desired state, and the program authorizes every
        // `true` against these allowlists — so once both entries are gone, `multicast unpublish`
        // asks for subscriber=true and `multicast unsubscribe` asks for publisher=true, and
        // neither is allowlisted any more. The roles are then stranded on the User with no legal
        // write to remove them. Detach the host first, then revoke.
        let dual_revoke: Vec<&str> = have_pub
            .difference(&want_pub)
            .filter(|code| have_sub.difference(&want_sub).any(|s| s == *code))
            .copied()
            .collect();
        if !dual_revoke.is_empty() {
            plan.blocked.push(BlockedItem {
                client_ip: entry.client_ip,
                user_payer: entry.user_payer,
                reason: format!(
                    "{} would leave both allowlists at once; detach the host \
                     (`doublezero multicast unpublish` / `unsubscribe`) before revoking both roles",
                    dual_revoke.join(", ")
                ),
            });
            continue;
        }

        // The pass admits one tenant, so the grant is its first entry.
        if tenants.is_none() && !pass.tenant_allowlist.is_empty() {
            tenants = Some(TenantCodes::read(client)?);
        }
        let have_tenant = pass.tenant_allowlist.first().and_then(|pk| {
            tenants
                .as_ref()
                .and_then(|t| t.by_pubkey.get(pk))
                .map(String::as_str)
        });
        let want_tenant = entry.ibrl.as_deref();
        // A declared ibrl also requires an unlimited epoch. Zero is not "expired" but "no epoch
        // defined", and blocks every unicast type outright, so a pass left that way fails
        // `connect ibrl` on the host long after this reported success.
        let epoch_drift = want_tenant.is_some() && pass.last_access_epoch != u64::MAX;

        if have_tenant != want_tenant || epoch_drift {
            plan.ibrl_changes.push(IbrlChange {
                client_ip: entry.client_ip,
                user_payer: entry.user_payer,
                access_pass: pass_pk,
                from: have_tenant.map(str::to_string),
                to: want_tenant.map(str::to_string),
                to_pk: want_tenant
                    .and_then(|code| tenants.as_ref().and_then(|t| t.by_code.get(code).copied()))
                    .unwrap_or_default(),
                epoch_drift,
            });
        } else if want_tenant.is_some() {
            plan.satisfied.push(SatisfiedGrant {
                client_ip: entry.client_ip,
                group: want_tenant.unwrap_or_default().to_string(),
                role: Role::Ibrl,
                source: "tenant_allowlist".to_string(),
            });
        }

        for (role, want, have) in [
            (Role::Publisher, &want_pub, &have_pub),
            (Role::Subscriber, &want_sub, &have_sub),
        ] {
            for code in want.difference(have) {
                // A feed already grants subscribe on its groups in its own metro, so granting it
                // again spends a transaction and changes nothing. Publisher is never feed-covered.
                if role == Role::Subscriber {
                    if let Some(feed_code) = feed_granted.get(*code) {
                        plan.satisfied.push(SatisfiedGrant {
                            client_ip: entry.client_ip,
                            group: (*code).to_string(),
                            role,
                            source: format!("feed {feed_code}"),
                        });
                        continue;
                    }
                }
                plan.changes.push(PlannedChange {
                    client_ip: entry.client_ip,
                    user_payer: entry.user_payer,
                    access_pass: pass_pk,
                    group: (*code).to_string(),
                    role,
                    op: Op::Grant,
                });
            }

            for code in want.intersection(have) {
                plan.satisfied.push(SatisfiedGrant {
                    client_ip: entry.client_ip,
                    group: (*code).to_string(),
                    role,
                    source: "allowlist".to_string(),
                });
            }

            for code in have.difference(want) {
                plan.changes.push(PlannedChange {
                    client_ip: entry.client_ip,
                    user_payer: entry.user_payer,
                    access_pass: pass_pk,
                    group: (*code).to_string(),
                    role,
                    op: Op::Revoke,
                });
            }
        }
    }

    Ok(plan)
}

fn allowlist_codes<'a>(
    allowlist: &[Pubkey],
    pk_to_code: &HashMap<Pubkey, &'a str>,
) -> BTreeSet<&'a str> {
    // A key with no group is a grant pointing at a deleted group. It cannot be named, and the
    // document cannot declare it, so leaving it out of the "have" set would plan a revoke the
    // operator cannot read. Skipping it leaves it untouched instead.
    allowlist
        .iter()
        .filter_map(|pk| pk_to_code.get(pk).copied())
        .collect()
}

/// Group code -> the feed that grants subscribe on it, for the feeds seated on this pass.
fn feed_granted_groups<'a>(
    pass: &AccessPass,
    feeds: Option<&HashMap<Pubkey, doublezero_sdk::Feed>>,
    pk_to_code: &HashMap<Pubkey, &'a str>,
) -> HashMap<&'a str, String> {
    let mut granted = HashMap::new();
    let Some(feeds) = feeds else {
        return granted;
    };
    for seat in pass.feed_seats() {
        let Some(feed) = feeds.get(&seat.feed_key) else {
            continue;
        };
        for group in &feed.groups {
            if let Some(code) = pk_to_code.get(group) {
                granted.entry(*code).or_insert_with(|| feed.code.clone());
            }
        }
    }
    granted
}

/// Renders the plan the way `terraform plan` does: the actions, then a one-line summary.
pub fn render_plan<W: Write>(
    out: &mut W,
    plan: &AccessPassPlan,
    verbose: bool,
) -> eyre::Result<()> {
    if plan.is_empty() && plan.blocked.is_empty() {
        writeln!(out, "No changes. The ledger matches the document.")?;
    } else if !plan.changes.is_empty() {
        writeln!(out, "DoubleZero will perform the following actions:")?;
        writeln!(out)?;

        let mut current: Option<(Ipv4Addr, Pubkey)> = None;
        for change in &plan.changes {
            let key = (change.client_ip, change.user_payer);
            if current != Some(key) {
                if current.is_some() {
                    writeln!(out)?;
                }
                writeln!(
                    out,
                    "  # access_pass {} / {}   ({})",
                    change.client_ip, change.user_payer, change.access_pass
                )?;
                current = Some(key);
            }
            let sign = match change.op {
                Op::Grant => '+',
                Op::Revoke => '-',
            };
            writeln!(
                out,
                "  {sign} {:<10}  {}",
                change.role.label(),
                change.group
            )?;
        }
        writeln!(out)?;
    }

    if !plan.ibrl_changes.is_empty() {
        writeln!(out, "IBRL (unicast) access:")?;
        writeln!(out)?;
        for change in &plan.ibrl_changes {
            writeln!(
                out,
                "  # access_pass {} / {}   ({})",
                change.client_ip, change.user_payer, change.access_pass
            )?;
            match (&change.from, &change.to) {
                (None, Some(to)) => writeln!(out, "  + ibrl        {to}")?,
                (Some(from), None) => writeln!(out, "  - ibrl        {from}")?,
                (Some(from), Some(to)) => writeln!(out, "  ~ ibrl        {from} -> {to}")?,
                // Only reachable when the tenant already matches and the epoch is what moved.
                (None, None) => writeln!(out, "  ~ ibrl        (epoch only)")?,
            }
            if change.epoch_drift {
                writeln!(out, "  ~ epoch       -> unlimited")?;
            }
        }
        writeln!(out)?;
    }

    if !plan.blocked.is_empty() {
        writeln!(out, "Blocked:")?;
        for item in &plan.blocked {
            writeln!(out, "  ! {} / {}", item.client_ip, item.user_payer)?;
            writeln!(out, "      {}", item.reason)?;
        }
        writeln!(out)?;
    }

    if verbose && !plan.satisfied.is_empty() {
        writeln!(out, "Already satisfied ({}):", plan.satisfied.len())?;
        for item in &plan.satisfied {
            writeln!(
                out,
                "    {} {:<10}  {:<28} {}",
                item.client_ip,
                item.role.label(),
                item.group,
                item.source
            )?;
        }
        writeln!(out)?;
    }

    if !plan.warnings.is_empty() {
        writeln!(out, "Warnings:")?;
        for warning in &plan.warnings {
            writeln!(out, "    {warning}")?;
        }
        writeln!(out)?;
    }

    writeln!(
        out,
        "Plan: {} to add, {} to remove, {} IBRL change(s), {} satisfied, {} blocked.",
        plan.grants(),
        plan.revokes(),
        plan.ibrl_changes.len(),
        plan.satisfied.len(),
        plan.blocked.len()
    )?;

    Ok(())
}

/// Reads an access-pass definition document and reports what would change, writing nothing.
#[derive(Args, Debug)]
pub struct PlanAccessPassCliCommand {
    /// Path to the access-pass definition document (YAML)
    #[arg(long, short = 'f')]
    pub file: PathBuf,
    /// Also list the grants that are already satisfied
    #[arg(long, default_value_t = false)]
    pub verbose: bool,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
}

impl PlanAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // No requirement check: this reads the ledger and writes nothing, so it must work for an
        // operator who holds no keypair and no admin permission.
        let document = AccessPassDocument::from_path(&self.file)?;
        let desired = document.resolve(client.get_payer())?;
        let plan = build_plan(client, &desired)?;

        if self.json {
            #[derive(Serialize)]
            struct PlanJson<'a> {
                changed: bool,
                #[serde(flatten)]
                plan: &'a AccessPassPlan,
            }
            let json = serde_json::to_string_pretty(&PlanJson {
                changed: !plan.is_empty(),
                plan: &plan,
            })?;
            writeln!(out, "{json}")?;
        } else {
            render_plan(out, &plan, self.verbose)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::{build_plan, render_plan, Op, Role};
    use crate::{accesspass::desired::DesiredAccessPass, tests::utils::create_test_client};
    use doublezero_sdk::{AccountType, Feed, MulticastGroup, MulticastGroupStatus};
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType, FeedSeat,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, net::Ipv4Addr};

    const IP: [u8; 4] = [203, 0, 113, 10];

    fn mgroup(code: &str) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: MulticastGroupStatus::Activated,
            code: code.to_string(),
            publisher_count: 0,
            subscriber_count: 0,
        }
    }

    fn pass(
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
        pub_allow: Vec<Pubkey>,
        sub_allow: Vec<Pubkey>,
    ) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: pub_allow,
            mgroup_sub_allowlist: sub_allow,
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        }
    }

    fn tenant(code: &str) -> doublezero_serviceability::state::tenant::Tenant {
        use doublezero_serviceability::state::tenant::{
            Tenant, TenantBillingConfig, TenantPaymentStatus,
        };
        Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: code.to_string(),
            vrf_id: 100,
            reference_count: 1,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
            include_topologies: vec![],
        }
    }

    fn with_tenants(
        client: &mut crate::doublezerocommand::MockCliCommand,
        codes: &[&str],
    ) -> HashMap<String, Pubkey> {
        let mut tenants = HashMap::new();
        let mut by_code = HashMap::new();
        for code in codes {
            let pk = Pubkey::new_unique();
            tenants.insert(pk, tenant(code));
            by_code.insert((*code).to_string(), pk);
        }
        client
            .expect_list_tenant()
            .returning(move |_| Ok(tenants.clone()));
        by_code
    }

    fn desired_ibrl(payer: Pubkey, ibrl: Option<&str>) -> Vec<DesiredAccessPass> {
        vec![DesiredAccessPass {
            client_ip: IP.into(),
            user_payer: payer,
            ibrl: ibrl.map(str::to_string),
            publish: vec![],
            subscribe: vec![],
        }]
    }

    fn desired(payer: Pubkey, publish: &[&str], subscribe: &[&str]) -> Vec<DesiredAccessPass> {
        vec![DesiredAccessPass {
            client_ip: IP.into(),
            user_payer: payer,
            ibrl: None,
            publish: publish.iter().map(|s| s.to_string()).collect(),
            subscribe: subscribe.iter().map(|s| s.to_string()).collect(),
        }]
    }

    /// Mock `list_multicastgroup` with the given codes, returning the code -> pubkey map.
    fn with_groups(
        client: &mut crate::doublezerocommand::MockCliCommand,
        codes: &[&str],
    ) -> HashMap<String, Pubkey> {
        let mut groups = HashMap::new();
        let mut by_code = HashMap::new();
        for code in codes {
            let pk = Pubkey::new_unique();
            groups.insert(pk, mgroup(code));
            by_code.insert((*code).to_string(), pk);
        }
        client
            .expect_list_multicastgroup()
            .returning(move |_| Ok(groups.clone()));
        by_code
    }

    #[test]
    fn plans_the_missing_grants_and_the_undeclared_revokes() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g-keep", "g-add", "g-drop"]);

        // Pass currently subscribes to g-keep and g-drop; the document declares g-keep and g-add.
        let existing = pass(
            IP.into(),
            payer,
            vec![],
            vec![by_code["g-keep"], by_code["g-drop"]],
        );
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g-keep", "g-add"])).unwrap();

        assert_eq!(plan.grants(), 1);
        assert_eq!(plan.revokes(), 1);
        let add = plan.changes.iter().find(|c| c.op == Op::Grant).unwrap();
        assert_eq!((add.group.as_str(), add.role), ("g-add", Role::Subscriber));
        let drop = plan.changes.iter().find(|c| c.op == Op::Revoke).unwrap();
        assert_eq!(
            (drop.group.as_str(), drop.role),
            ("g-drop", Role::Subscriber)
        );
        // g-keep is already there and is reported as satisfied rather than re-granted.
        assert!(plan
            .satisfied
            .iter()
            .any(|s| s.group == "g-keep" && s.source == "allowlist"));
    }

    #[test]
    fn a_matching_document_plans_nothing() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g1"]);

        let existing = pass(IP.into(), payer, vec![by_code["g1"]], vec![by_code["g1"]]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &["g1"], &["g1"])).unwrap();

        assert!(plan.is_empty(), "{:?}", plan.changes);
        assert_eq!(plan.satisfied.len(), 2);
    }

    #[test]
    fn a_feed_granted_subscribe_is_satisfied_not_granted() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g-feed"]);
        let group_pk = by_code["g-feed"];

        let feed_key = Pubkey::new_unique();
        let mut edge = pass(IP.into(), payer, vec![], vec![]);
        edge.accesspass_type = AccessPassType::EdgeSeat(vec![FeedSeat {
            feed_key,
            max_users: 1,
            max_future_users: 1,
            current_users: 0,
            anniversary_day: 1,
            window_end: 0,
            terminates_at: 0,
        }]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), edge.clone()))));
        client.expect_list_feed().returning(move |_| {
            Ok(HashMap::from([(
                feed_key,
                Feed {
                    account_type: AccountType::Feed,
                    owner: Pubkey::new_unique(),
                    bump_seed: 1,
                    code: "example-feed".to_string(),
                    name: "QA payments".to_string(),
                    exchange: Pubkey::new_unique(),
                    groups: vec![group_pk],
                },
            )]))
        });

        let plan = build_plan(&client, &desired(payer, &["g-feed"], &["g-feed"])).unwrap();

        // Subscribe is already covered by the feed, so no transaction for it...
        assert!(plan
            .satisfied
            .iter()
            .any(|s| s.role == Role::Subscriber && s.source == "feed example-feed"));
        // ...but a feed grants subscribe only, so publish is still a real gap.
        assert_eq!(plan.grants(), 1);
        assert_eq!(plan.changes[0].role, Role::Publisher);
    }

    #[test]
    fn a_missing_access_pass_is_blocked_rather_than_created() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &["g1"]);
        client.expect_get_accesspass().returning(|_| Ok(None));

        let plan = build_plan(&client, &desired(payer, &[], &["g1"])).unwrap();

        assert!(plan.is_empty());
        assert_eq!(plan.blocked.len(), 1);
        assert!(plan.blocked[0].reason.contains("no access pass"));
    }

    #[test]
    fn revoking_both_roles_of_one_group_is_blocked() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g-both"]);

        let existing = pass(
            IP.into(),
            payer,
            vec![by_code["g-both"]],
            vec![by_code["g-both"]],
        );
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        // The document declares nothing, so g-both would leave both allowlists at once.
        let plan = build_plan(&client, &desired(payer, &[], &[])).unwrap();

        assert!(
            plan.is_empty(),
            "no writes may be planned: {:?}",
            plan.changes
        );
        assert_eq!(plan.blocked.len(), 1);
        assert!(plan.blocked[0].reason.contains("both allowlists"));
    }

    #[test]
    fn an_unknown_group_code_fails_before_anything_is_planned() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &["g1"]);

        let err = build_plan(&client, &desired(payer, &[], &["g1", "typo", "worse"])).unwrap_err();

        // Every bad code is named, so a fix does not have to be discovered one round trip at a time.
        assert!(err.to_string().contains("typo"), "{err}");
        assert!(err.to_string().contains("worse"), "{err}");
    }

    #[test]
    fn landing_on_the_shared_wildcard_pass_is_warned_about() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &["g1"]);

        // Resolution prefers the 0.0.0.0 pass, so a concrete IP can land on the shared one.
        let shared = pass(Ipv4Addr::UNSPECIFIED, payer, vec![], vec![]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), shared.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g1"])).unwrap();

        assert_eq!(plan.warnings.len(), 1);
        assert!(
            plan.warnings[0].contains("shared access pass"),
            "{:?}",
            plan.warnings
        );
    }

    #[test]
    fn a_group_that_no_longer_exists_is_left_alone() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g1"]);

        // A key with no group behind it cannot be named, so the document cannot declare it and
        // planning a revoke for it would print an unreadable pubkey.
        let existing = pass(
            IP.into(),
            payer,
            vec![],
            vec![by_code["g1"], Pubkey::new_unique()],
        );
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g1"])).unwrap();

        assert!(plan.is_empty(), "{:?}", plan.changes);
    }

    #[test]
    fn grants_the_declared_tenant_and_pins_the_epoch() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        let by_code = with_tenants(&mut client, &["solana"]);

        // No tenant, and an epoch that is not unlimited.
        let mut existing = pass(IP.into(), payer, vec![], vec![]);
        existing.last_access_epoch = 200;
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired_ibrl(payer, Some("solana"))).unwrap();

        assert_eq!(plan.ibrl_changes.len(), 1);
        let change = &plan.ibrl_changes[0];
        assert_eq!(change.from, None);
        assert_eq!(change.to.as_deref(), Some("solana"));
        assert_eq!(change.to_pk, by_code["solana"]);
        assert!(change.epoch_drift);
    }

    #[test]
    fn a_matching_tenant_with_an_unlimited_epoch_is_satisfied() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        let by_code = with_tenants(&mut client, &["solana"]);

        let mut existing = pass(IP.into(), payer, vec![], vec![]);
        existing.tenant_allowlist = vec![by_code["solana"]];
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired_ibrl(payer, Some("solana"))).unwrap();

        assert!(plan.is_empty(), "{:?}", plan.ibrl_changes);
        assert!(plan
            .satisfied
            .iter()
            .any(|s| s.role == Role::Ibrl && s.source == "tenant_allowlist"));
    }

    #[test]
    fn a_finite_epoch_alone_is_drift() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        let by_code = with_tenants(&mut client, &["solana"]);

        // The tenant is already right; only the epoch is wrong. Left unrepaired, `connect ibrl`
        // on this host fails at an unpredictable date.
        let mut existing = pass(IP.into(), payer, vec![], vec![]);
        existing.tenant_allowlist = vec![by_code["solana"]];
        existing.last_access_epoch = 0;
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired_ibrl(payer, Some("solana"))).unwrap();

        assert_eq!(plan.ibrl_changes.len(), 1);
        assert!(plan.ibrl_changes[0].epoch_drift);
        assert_eq!(plan.ibrl_changes[0].to.as_deref(), Some("solana"));
    }

    #[test]
    fn an_omitted_ibrl_clears_the_tenant() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        let by_code = with_tenants(&mut client, &["solana"]);

        let mut existing = pass(IP.into(), payer, vec![], vec![]);
        existing.tenant_allowlist = vec![by_code["solana"]];
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        // The document declares no ibrl, and every field here is declarative.
        let mut want = desired_ibrl(payer, None);
        want[0].subscribe = vec![];
        let plan = build_plan(&client, &want).unwrap();

        assert_eq!(plan.ibrl_changes.len(), 1);
        assert_eq!(plan.ibrl_changes[0].from.as_deref(), Some("solana"));
        assert_eq!(plan.ibrl_changes[0].to, None);
        assert_eq!(plan.ibrl_changes[0].to_pk, Pubkey::default());
        // Clearing does not touch the epoch.
        assert!(!plan.ibrl_changes[0].epoch_drift);
    }

    #[test]
    fn an_unknown_tenant_code_fails_before_anything_is_planned() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        with_tenants(&mut client, &["solana"]);

        let err = build_plan(&client, &desired_ibrl(payer, Some("solanaa"))).unwrap_err();
        assert!(err.to_string().contains("solanaa"), "{err}");
    }

    #[test]
    fn a_document_with_no_ibrl_skips_the_tenant_scan_when_no_pass_has_one() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g1"]);
        client.expect_list_tenant().never();

        let existing = pass(IP.into(), payer, vec![], vec![by_code["g1"]]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g1"])).unwrap();
        assert!(plan.is_empty());
    }

    #[test]
    fn a_document_with_no_ibrl_still_reads_tenants_when_a_pass_carries_one() {
        // The scan cannot be skipped merely because the document is silent: a pass with a tenant
        // still has to be cleared, and naming it in the plan needs its code.
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        with_groups(&mut client, &[]);
        let by_code = with_tenants(&mut client, &["solana"]);

        let mut existing = pass(IP.into(), payer, vec![], vec![]);
        existing.tenant_allowlist = vec![by_code["solana"]];
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &[])).unwrap();

        assert_eq!(plan.ibrl_changes.len(), 1);
        assert_eq!(plan.ibrl_changes[0].from.as_deref(), Some("solana"));
    }

    #[test]
    fn render_reports_no_changes_when_the_ledger_matches() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g1"]);
        let existing = pass(IP.into(), payer, vec![], vec![by_code["g1"]]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g1"])).unwrap();
        let mut out = Vec::new();
        render_plan(&mut out, &plan, false).unwrap();

        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("No changes."), "{text}");
        assert!(text.contains("Plan: 0 to add, 0 to remove"), "{text}");
    }

    #[test]
    fn render_shows_the_actions_grouped_by_pass() {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let by_code = with_groups(&mut client, &["g-add", "g-drop"]);
        let existing = pass(IP.into(), payer, vec![], vec![by_code["g-drop"]]);
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), existing.clone()))));

        let plan = build_plan(&client, &desired(payer, &[], &["g-add"])).unwrap();
        let mut out = Vec::new();
        render_plan(&mut out, &plan, false).unwrap();

        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("# access_pass 203.0.113.10"), "{text}");
        assert!(text.contains("+ subscriber  g-add"), "{text}");
        assert!(text.contains("- subscriber  g-drop"), "{text}");
        assert!(text.contains("Plan: 1 to add, 1 to remove"), "{text}");
    }
}
