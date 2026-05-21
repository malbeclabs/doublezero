use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::commands::resource::{
    allocate::AllocateResourceCommand, closeaccount::CloseResourceByPubkeyCommand,
    create::CreateResourceCommand, deallocate::DeallocateResourceCommand,
};
use doublezero_serviceability::{
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    state::{
        accountdata::AccountData,
        device::Device,
        interface::{InterfaceType, LoopbackType},
        link::Link,
        multicastgroup::MulticastGroup,
        resource_extension::{Allocator, ResourceExtensionOwned},
        user::{User, UserType},
    },
};
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::{HashMap, HashSet},
    io::{BufRead, Write},
};

/// Represents a discrepancy found during resource verification
#[derive(Debug, Clone)]
pub enum ResourceDiscrepancy {
    /// Resource is allocated in extension but not used by any account
    AllocatedButNotUsed {
        resource_type: ResourceType,
        value: IdOrIp,
    },
    /// Resource is used by an account but not allocated in extension
    UsedButNotAllocated {
        resource_type: ResourceType,
        value: IdOrIp,
        account_pubkey: Pubkey,
        account_type: String,
    },
    /// Resource extension account not found
    ExtensionNotFound { resource_type: ResourceType },
    /// Resource is used by multiple accounts (duplicate usage). `accounts`
    /// lists every owner in insertion order; `accounts.len() >= 2`.
    DuplicateUsage {
        resource_type: ResourceType,
        value: IdOrIp,
        accounts: Vec<(Pubkey, String)>,
    },
    /// ResourceExtension account exists onchain but does not correspond to any
    /// currently-expected PDA (global singleton or per-device extension for a
    /// live device/prefix). Typically caused by device deletion or a shrunk
    /// dz_prefixes list.
    OrphanedExtension {
        pubkey: Pubkey,
        associated_with: Pubkey,
        owner: Pubkey,
        allocator_kind: &'static str,
    },
}

/// Result of resource verification
#[derive(Debug, Default)]
pub struct VerifyResourceResult {
    pub discrepancies: Vec<ResourceDiscrepancy>,
    pub user_tunnel_block_checked: usize,
    pub tunnel_ids_checked: usize,
    pub dz_prefix_block_checked: usize,
    pub device_tunnel_block_checked: usize,
    pub segment_routing_ids_checked: usize,
    pub link_ids_checked: usize,
    pub multicast_group_block_checked: usize,
    pub multicast_publisher_block_checked: usize,
    /// Pubkey → human-readable code, populated for devices and links so the
    /// display layer can print `code (pubkey)` instead of raw pubkeys.
    pub pubkey_labels: HashMap<Pubkey, String>,
}

impl VerifyResourceResult {
    pub fn is_ok(&self) -> bool {
        self.discrepancies.is_empty()
    }
}

#[derive(Args, Debug, Default)]
pub struct VerifyResourceCliCommand {
    /// Automatically fix discrepancies after confirmation
    #[arg(long)]
    pub fix: bool,
}

impl VerifyResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let result = verify_resources(client)?;

        // Print summary
        writeln!(out, "Resource Verification Report")?;
        writeln!(out, "============================")?;
        writeln!(out)?;

        writeln!(out, "Resources checked:")?;
        writeln!(
            out,
            "  UserTunnelBlock:     {}",
            result.user_tunnel_block_checked
        )?;
        writeln!(out, "  TunnelIds:           {}", result.tunnel_ids_checked)?;
        writeln!(
            out,
            "  DzPrefixBlock:       {}",
            result.dz_prefix_block_checked
        )?;
        writeln!(
            out,
            "  DeviceTunnelBlock:   {}",
            result.device_tunnel_block_checked
        )?;
        writeln!(
            out,
            "  SegmentRoutingIds:   {}",
            result.segment_routing_ids_checked
        )?;
        writeln!(out, "  LinkIds:             {}", result.link_ids_checked)?;
        writeln!(
            out,
            "  MulticastGroupBlock: {}",
            result.multicast_group_block_checked
        )?;
        writeln!(
            out,
            "  MulticastPublisherBlock: {}",
            result.multicast_publisher_block_checked
        )?;
        writeln!(out)?;

        if result.is_ok() {
            writeln!(out, "No discrepancies found.")?;
        } else {
            writeln!(out, "Discrepancies found: {}", result.discrepancies.len())?;
            writeln!(out)?;

            // Group discrepancies by type
            let mut allocated_not_used: Vec<&ResourceDiscrepancy> = Vec::new();
            let mut used_not_allocated: Vec<&ResourceDiscrepancy> = Vec::new();
            let mut extensions_not_found: Vec<&ResourceDiscrepancy> = Vec::new();
            let mut duplicate_usages: Vec<&ResourceDiscrepancy> = Vec::new();
            let mut orphaned_extensions: Vec<&ResourceDiscrepancy> = Vec::new();

            for d in &result.discrepancies {
                match d {
                    ResourceDiscrepancy::AllocatedButNotUsed { .. } => {
                        allocated_not_used.push(d);
                    }
                    ResourceDiscrepancy::UsedButNotAllocated { .. } => {
                        used_not_allocated.push(d);
                    }
                    ResourceDiscrepancy::ExtensionNotFound { .. } => {
                        extensions_not_found.push(d);
                    }
                    ResourceDiscrepancy::DuplicateUsage { .. } => {
                        duplicate_usages.push(d);
                    }
                    ResourceDiscrepancy::OrphanedExtension { .. } => {
                        orphaned_extensions.push(d);
                    }
                }
            }

            if !extensions_not_found.is_empty() {
                writeln!(out, "Resource Extensions Not Found:")?;
                writeln!(out, "------------------------------")?;
                for d in &extensions_not_found {
                    if let ResourceDiscrepancy::ExtensionNotFound { resource_type } = d {
                        writeln!(out, "  {}", resource_type)?;
                    }
                }
                if !self.fix {
                    writeln!(
                        out,
                        "  Hint: use --fix to create missing resource extensions."
                    )?;
                }
                writeln!(out)?;
            }

            if !allocated_not_used.is_empty() {
                writeln!(
                    out,
                    "Allocated but not used (may indicate orphaned allocations):"
                )?;
                writeln!(
                    out,
                    "-----------------------------------------------------------"
                )?;
                for d in &allocated_not_used {
                    if let ResourceDiscrepancy::AllocatedButNotUsed {
                        resource_type,
                        value,
                    } = d
                    {
                        writeln!(out, "  {} = {}", resource_type, value)?;
                    }
                }
                writeln!(out)?;
            }

            if !used_not_allocated.is_empty() {
                writeln!(
                    out,
                    "Used but not allocated (may indicate missing allocations):"
                )?;
                writeln!(
                    out,
                    "----------------------------------------------------------"
                )?;
                for d in &used_not_allocated {
                    if let ResourceDiscrepancy::UsedButNotAllocated {
                        resource_type,
                        value,
                        account_pubkey,
                        account_type,
                    } = d
                    {
                        writeln!(
                            out,
                            "  {} = {} (used by {} {})",
                            resource_type,
                            value,
                            account_type,
                            format_pubkey(account_pubkey, &result.pubkey_labels)
                        )?;
                    }
                }
                writeln!(out)?;
            }

            if !orphaned_extensions.is_empty() {
                writeln!(
                    out,
                    "Orphaned resource extensions (not tied to any live device/prefix or global type):"
                )?;
                writeln!(
                    out,
                    "----------------------------------------------------------------------------------"
                )?;
                for d in &orphaned_extensions {
                    if let ResourceDiscrepancy::OrphanedExtension {
                        pubkey,
                        associated_with,
                        owner: _,
                        allocator_kind,
                    } = d
                    {
                        writeln!(
                            out,
                            "  {} (allocator={}, associated_with={})",
                            pubkey,
                            allocator_kind,
                            format_pubkey(associated_with, &result.pubkey_labels)
                        )?;
                    }
                }
                if !self.fix {
                    writeln!(
                        out,
                        "  Hint: use --fix to close orphaned resource extensions."
                    )?;
                }
                writeln!(out)?;
            }

            if !duplicate_usages.is_empty() {
                writeln!(
                    out,
                    "Duplicate usage (same resource used by multiple accounts):"
                )?;
                writeln!(
                    out,
                    "-----------------------------------------------------------"
                )?;
                for d in &duplicate_usages {
                    if let ResourceDiscrepancy::DuplicateUsage {
                        resource_type,
                        value,
                        accounts,
                    } = d
                    {
                        let owners = accounts
                            .iter()
                            .map(|(pk, ty)| {
                                format!("{} {}", ty, format_pubkey(pk, &result.pubkey_labels))
                            })
                            .collect::<Vec<_>>()
                            .join(", ");
                        writeln!(
                            out,
                            "  {} = {} (used by {} owners: {})",
                            resource_type,
                            value,
                            accounts.len(),
                            owners
                        )?;
                    }
                }
                writeln!(out)?;
            }

            // Handle --fix flag
            if self.fix {
                // Step 1: Create missing resource extensions
                if !extensions_not_found.is_empty() {
                    writeln!(out, "Creating missing resource extensions...")?;
                    for d in &extensions_not_found {
                        if let ResourceDiscrepancy::ExtensionNotFound { resource_type } = d {
                            write!(out, "  CREATE {} ...", resource_type)?;
                            let cmd = CreateResourceCommand {
                                resource_type: *resource_type,
                            };
                            match client.create_resource(cmd) {
                                Ok(sig) => {
                                    writeln!(out, " OK (signature: {})", sig)?;
                                }
                                Err(e) => {
                                    writeln!(out, " FAILED: {}", e)?;
                                }
                            }
                        }
                    }
                    writeln!(out)?;
                }

                // Re-verify to pick up newly created extensions (or use
                // original result if no extensions were missing). We clone
                // discrepancies into owned vectors so lifetimes are clean.
                let fix_discrepancies = if !extensions_not_found.is_empty() {
                    writeln!(out, "Re-verifying resources after extension creation...")?;
                    let fresh = verify_resources(client)?;
                    writeln!(out)?;
                    fresh.discrepancies
                } else {
                    result.discrepancies.clone()
                };

                let mut fix_allocated_not_used: Vec<&ResourceDiscrepancy> = Vec::new();
                let mut fix_used_not_allocated: Vec<&ResourceDiscrepancy> = Vec::new();
                let mut fix_duplicate_usages: Vec<&ResourceDiscrepancy> = Vec::new();
                let mut fix_orphaned_extensions: Vec<&ResourceDiscrepancy> = Vec::new();

                for d in &fix_discrepancies {
                    match d {
                        ResourceDiscrepancy::AllocatedButNotUsed { .. } => {
                            fix_allocated_not_used.push(d);
                        }
                        ResourceDiscrepancy::UsedButNotAllocated { .. } => {
                            fix_used_not_allocated.push(d);
                        }
                        ResourceDiscrepancy::DuplicateUsage { .. } => {
                            fix_duplicate_usages.push(d);
                        }
                        ResourceDiscrepancy::OrphanedExtension { .. } => {
                            fix_orphaned_extensions.push(d);
                        }
                        _ => {}
                    }
                }

                // Step 2: Warn about duplicate usages. The duplicate itself must
                // be resolved manually (by changing one of the conflicting
                // accounts), but the underlying allocation state is still
                // corrected below: the shared value stays reserved in the
                // extension so it cannot be handed out to a third account
                // before the conflict is resolved.
                if !fix_duplicate_usages.is_empty() {
                    writeln!(
                        out,
                        "Warning: duplicate usages detected (must be resolved manually):"
                    )?;
                    for d in &fix_duplicate_usages {
                        if let ResourceDiscrepancy::DuplicateUsage {
                            resource_type,
                            value,
                            accounts,
                        } = d
                        {
                            let owners = accounts
                                .iter()
                                .map(|(pk, ty)| {
                                    format!("{} {}", ty, format_pubkey(pk, &result.pubkey_labels))
                                })
                                .collect::<Vec<_>>()
                                .join(", ");
                            writeln!(
                                out,
                                "  {} = {} (used by {} owners: {})",
                                resource_type,
                                value,
                                accounts.len(),
                                owners
                            )?;
                        }
                    }
                    writeln!(out)?;
                }

                // Step 3: Fix allocate/deallocate discrepancies. Values flagged
                // as DuplicateUsage are not excluded — a shared value still
                // belongs in the allocated set.
                let fixable_allocated_not_used: Vec<_> = fix_allocated_not_used.iter().collect();
                let fixable_used_not_allocated: Vec<_> = fix_used_not_allocated.iter().collect();

                if !fixable_allocated_not_used.is_empty()
                    || !fixable_used_not_allocated.is_empty()
                    || !fix_orphaned_extensions.is_empty()
                {
                    writeln!(out, "Proposed fixes:")?;
                    writeln!(out, "--------------")?;

                    for d in &fixable_allocated_not_used {
                        if let ResourceDiscrepancy::AllocatedButNotUsed {
                            resource_type,
                            value,
                        } = d
                        {
                            writeln!(out, "  DEALLOCATE {} = {}", resource_type, value)?;
                        }
                    }

                    for d in &fixable_used_not_allocated {
                        if let ResourceDiscrepancy::UsedButNotAllocated {
                            resource_type,
                            value,
                            ..
                        } = d
                        {
                            writeln!(out, "  ALLOCATE {} = {}", resource_type, value)?;
                        }
                    }

                    for d in &fix_orphaned_extensions {
                        if let ResourceDiscrepancy::OrphanedExtension { pubkey, .. } = d {
                            writeln!(out, "  CLOSE ResourceExtension {}", pubkey)?;
                        }
                    }

                    writeln!(out)?;
                    write!(out, "Should this be fixed? [y/N]: ")?;
                    out.flush()?;

                    let stdin = std::io::stdin();
                    let mut input = String::new();
                    stdin.lock().read_line(&mut input)?;

                    if input.trim().eq_ignore_ascii_case("y")
                        || input.trim().eq_ignore_ascii_case("yes")
                    {
                        writeln!(out)?;
                        writeln!(out, "Applying fixes...")?;

                        // Deallocate orphaned allocations
                        for d in &fixable_allocated_not_used {
                            if let ResourceDiscrepancy::AllocatedButNotUsed {
                                resource_type,
                                value,
                            } = d
                            {
                                writeln!(out, "  Deallocating {} = {} ...", resource_type, value)?;
                                let cmd = DeallocateResourceCommand {
                                    resource_type: *resource_type,
                                    value: value.clone(),
                                };
                                match client.deallocate_resource(cmd) {
                                    Ok(sig) => {
                                        writeln!(out, "    OK (signature: {})", sig)?;
                                    }
                                    Err(e) => {
                                        writeln!(out, "    FAILED: {}", e)?;
                                    }
                                }
                            }
                        }

                        // Allocate missing allocations
                        for d in &fixable_used_not_allocated {
                            if let ResourceDiscrepancy::UsedButNotAllocated {
                                resource_type,
                                value,
                                ..
                            } = d
                            {
                                writeln!(out, "  Allocating {} = {} ...", resource_type, value)?;
                                let cmd = AllocateResourceCommand {
                                    resource_type: *resource_type,
                                    requested: Some(value.clone()),
                                };
                                match client.allocate_resource(cmd) {
                                    Ok(sig) => {
                                        writeln!(out, "    OK (signature: {})", sig)?;
                                    }
                                    Err(e) => {
                                        writeln!(out, "    FAILED: {}", e)?;
                                    }
                                }
                            }
                        }

                        // Close orphaned extensions
                        for d in &fix_orphaned_extensions {
                            if let ResourceDiscrepancy::OrphanedExtension {
                                pubkey, owner, ..
                            } = d
                            {
                                writeln!(out, "  Closing ResourceExtension {} ...", pubkey)?;
                                let cmd = CloseResourceByPubkeyCommand {
                                    pubkey: *pubkey,
                                    owner: *owner,
                                };
                                match client.close_resource_by_pubkey(cmd) {
                                    Ok(sig) => {
                                        writeln!(out, "    OK (signature: {})", sig)?;
                                    }
                                    Err(e) => {
                                        writeln!(out, "    FAILED: {}", e)?;
                                    }
                                }
                            }
                        }

                        writeln!(out)?;
                        writeln!(out, "Done.")?;
                    } else {
                        writeln!(out, "Aborted.")?;
                    }
                }
            }
        }

        Ok(())
    }
}

/// Verify resources and return discrepancies
fn verify_resources<C: CliCommand>(client: &C) -> eyre::Result<VerifyResourceResult> {
    let program_id = client.get_program_id();

    // Fetch all accounts
    let all_accounts = client.get_all()?;

    // Categorize accounts
    let mut users: HashMap<Pubkey, User> = HashMap::new();
    let mut devices: HashMap<Pubkey, Device> = HashMap::new();
    let mut links: HashMap<Pubkey, Link> = HashMap::new();
    let mut multicast_groups: HashMap<Pubkey, MulticastGroup> = HashMap::new();
    let mut resource_extensions: HashMap<Pubkey, ResourceExtensionOwned> = HashMap::new();

    for (pubkey, account) in all_accounts {
        match *account {
            AccountData::User(user) => {
                users.insert(*pubkey, user);
            }
            AccountData::Device(device) => {
                devices.insert(*pubkey, device);
            }
            AccountData::Link(link) => {
                links.insert(*pubkey, link);
            }
            AccountData::MulticastGroup(group) => {
                multicast_groups.insert(*pubkey, group);
            }
            AccountData::ResourceExtension(ext) => {
                resource_extensions.insert(*pubkey, ext);
            }
            _ => {}
        }
    }

    let mut result = VerifyResourceResult::default();

    // Build pubkey → code labels so the display layer can annotate device and
    // link pubkeys with their human-readable codes.
    for (pk, device) in &devices {
        if !device.code.is_empty() {
            result.pubkey_labels.insert(*pk, device.code.clone());
        }
    }
    for (pk, link) in &links {
        if !link.code.is_empty() {
            result.pubkey_labels.insert(*pk, link.code.clone());
        }
    }

    // Verify UserTunnelBlock
    verify_user_tunnel_block(&program_id, &users, &resource_extensions, &mut result);

    // Verify TunnelIds (per-device)
    verify_tunnel_ids(
        &program_id,
        &users,
        &devices,
        &resource_extensions,
        &mut result,
    );

    // Verify DzPrefixBlock (per-device, per-prefix)
    verify_dz_prefix_block(
        &program_id,
        &users,
        &devices,
        &resource_extensions,
        &mut result,
    );

    // Verify DeviceTunnelBlock (from device loopback interfaces and link tunnel_net)
    verify_device_tunnel_block(
        &program_id,
        &devices,
        &links,
        &resource_extensions,
        &mut result,
    );

    // Verify SegmentRoutingIds
    verify_segment_routing_ids(&program_id, &devices, &resource_extensions, &mut result);

    // Verify LinkIds
    verify_link_ids(&program_id, &links, &resource_extensions, &mut result);

    // Verify MulticastGroupBlock
    verify_multicast_group_block(
        &program_id,
        &multicast_groups,
        &resource_extensions,
        &mut result,
    );

    // Verify MulticastPublisherBlock
    verify_multicast_publisher_block(&program_id, &users, &resource_extensions, &mut result);

    // Detect orphaned extensions whose PDA doesn't match any currently-expected
    // resource type for live state.
    detect_orphaned_extensions(&program_id, &devices, &resource_extensions, &mut result);

    Ok(result)
}

/// Build the set of PDAs the program is expected to own right now (every global
/// singleton plus per-device extensions for each live device and dz_prefix
/// index), then flag any loaded ResourceExtension whose key is not in that set.
fn detect_orphaned_extensions(
    program_id: &Pubkey,
    devices: &HashMap<Pubkey, Device>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let mut expected: HashSet<Pubkey> = HashSet::new();

    // Global singletons. VrfIds and AdminGroupBits aren't verified against
    // usage above but must still be treated as legitimate, not orphans.
    for resource_type in [
        ResourceType::DeviceTunnelBlock,
        ResourceType::UserTunnelBlock,
        ResourceType::MulticastGroupBlock,
        ResourceType::MulticastPublisherBlock,
        ResourceType::LinkIds,
        ResourceType::SegmentRoutingIds,
        ResourceType::VrfIds,
        ResourceType::AdminGroupBits,
    ] {
        let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);
        expected.insert(pda);
    }

    // Per-device: TunnelIds(device, 0) + DzPrefixBlock(device, i) for each prefix.
    for (device_pk, device) in devices {
        let (tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device_pk, 0));
        expected.insert(tunnel_pda);

        for index in 0..device.dz_prefixes.len() {
            let (prefix_pda, _, _) = get_resource_extension_pda(
                program_id,
                ResourceType::DzPrefixBlock(*device_pk, index),
            );
            expected.insert(prefix_pda);
        }
    }

    for (pda, ext) in resource_extensions {
        if expected.contains(pda) {
            continue;
        }
        let allocator_kind = match ext.allocator {
            Allocator::Ip(_) => "Ip",
            Allocator::Id(_) => "Id",
        };
        result
            .discrepancies
            .push(ResourceDiscrepancy::OrphanedExtension {
                pubkey: *pda,
                associated_with: ext.associated_with,
                owner: ext.owner,
                allocator_kind,
            });
    }
}

fn verify_user_tunnel_block(
    program_id: &Pubkey,
    users: &HashMap<Pubkey, User>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::UserTunnelBlock;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    // Build set of allocated IPs
    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    // Build set of in-use IPs from users
    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();
    for (user_pk, user) in users {
        let tunnel_ip = user.tunnel_net.ip();
        if !tunnel_ip.is_unspecified() && user.tunnel_net.prefix() > 0 {
            // Iterate over all IPs in the network (e.g., /31 has 2 IPs)
            for i in 0..user.tunnel_net.size() {
                if let Some(ip) = user.tunnel_net.nth(i) {
                    let ip_net = NetworkV4::new(ip, 32).unwrap();
                    let id_or_ip = IdOrIp::Ip(ip_net);
                    insert_usage(&mut in_use, id_or_ip, *user_pk, "User".to_string());
                }
            }
            result.user_tunnel_block_checked += 1;
        }
    }

    // Check for discrepancies
    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn verify_tunnel_ids(
    program_id: &Pubkey,
    users: &HashMap<Pubkey, User>,
    devices: &HashMap<Pubkey, Device>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    // Group users by device
    let mut users_by_device: HashMap<Pubkey, Vec<(Pubkey, &User)>> = HashMap::new();
    for (user_pk, user) in users {
        if user.device_pk != Pubkey::default() {
            users_by_device
                .entry(user.device_pk)
                .or_default()
                .push((*user_pk, user));
        }
    }

    // Check TunnelIds for each device that has users
    for device_pk in devices.keys() {
        let resource_type = ResourceType::TunnelIds(*device_pk, 0);
        let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

        let Some(extension) = resource_extensions.get(&pda) else {
            result
                .discrepancies
                .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
            continue;
        };

        let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

        let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();
        if let Some(device_users) = users_by_device.get(device_pk) {
            for (user_pk, user) in device_users {
                if user.tunnel_id != 0 {
                    let id_or_ip = IdOrIp::Id(user.tunnel_id);
                    insert_usage(&mut in_use, id_or_ip, *user_pk, "User".to_string());
                    result.tunnel_ids_checked += 1;
                }
            }
        }

        check_discrepancies(resource_type, &allocated, &in_use, result);
    }
}

fn verify_dz_prefix_block(
    program_id: &Pubkey,
    users: &HashMap<Pubkey, User>,
    devices: &HashMap<Pubkey, Device>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    // For each device, check each dz_prefix block
    for (device_pk, device) in devices {
        for (index, prefix) in device.dz_prefixes.iter().enumerate() {
            let resource_type = ResourceType::DzPrefixBlock(*device_pk, index);
            let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

            let Some(extension) = resource_extensions.get(&pda) else {
                result
                    .discrepancies
                    .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
                continue;
            };

            let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

            // Find users whose dz_ip falls within this prefix
            let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();

            // First IP is reserved for the device itself (Loopback100)
            let first_ip = prefix.ip();
            let first_ip_net = NetworkV4::new(first_ip, 32).unwrap();
            insert_usage(
                &mut in_use,
                IdOrIp::Ip(first_ip_net),
                *device_pk,
                "Device (reserved)".to_string(),
            );

            for (user_pk, user) in users {
                if user.device_pk != *device_pk {
                    continue;
                }

                let dz_ip = user.dz_ip;
                // Check conditions: dz_ip != client_ip AND dz_ip != UNSPECIFIED
                if dz_ip == user.client_ip || dz_ip.is_unspecified() {
                    continue;
                }

                // Check if this dz_ip falls within this prefix
                if prefix.contains(dz_ip) {
                    let ip_net = NetworkV4::new(dz_ip, 32).unwrap();
                    let id_or_ip = IdOrIp::Ip(ip_net);
                    insert_usage(&mut in_use, id_or_ip, *user_pk, "User".to_string());
                    result.dz_prefix_block_checked += 1;
                }
            }

            check_discrepancies(resource_type, &allocated, &in_use, result);
        }
    }
}

fn verify_device_tunnel_block(
    program_id: &Pubkey,
    devices: &HashMap<Pubkey, Device>,
    links: &HashMap<Pubkey, Link>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::DeviceTunnelBlock;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    // Pull the base network so we can ignore loopback/link IPs that fall
    // outside the block's range. Both interface and link processors honor a
    // caller-supplied ip_net/tunnel_net and skip the allocator in that path
    // (see `processors/device/interface/create.rs` and
    // `processors/link/resource_onchain_helpers.rs`) — most commonly for
    // user-tunnel-endpoint loopbacks that land on a globally routable IP.
    // Those IPs aren't in the DeviceTunnelBlock bitmap and would otherwise
    // be falsely reported as `UsedButNotAllocated`.
    let base_net = match &extension.allocator {
        Allocator::Ip(ip_alloc) => ip_alloc.base_net,
        Allocator::Id(_) => return,
    };

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();

    // Check device loopback interfaces (only vpnv4/ipv4 loopback types)
    for (device_pk, device) in devices {
        for iface in &device.interfaces {
            if iface.interface_type == InterfaceType::Loopback
                && (iface.loopback_type == LoopbackType::Vpnv4
                    || iface.loopback_type == LoopbackType::Ipv4)
            {
                let ip = iface.ip_net.ip();
                if !ip.is_unspecified() && iface.ip_net.prefix() > 0 {
                    // Iterate over all IPs in the network
                    for i in 0..iface.ip_net.size() {
                        if let Some(ip) = iface.ip_net.nth(i) {
                            if !base_net.contains(ip) {
                                continue;
                            }
                            let ip_net = NetworkV4::new(ip, 32).unwrap();
                            let id_or_ip = IdOrIp::Ip(ip_net);
                            insert_usage(
                                &mut in_use,
                                id_or_ip,
                                *device_pk,
                                format!("Device interface {}", iface.name),
                            );
                        }
                    }
                    result.device_tunnel_block_checked += 1;
                }
            }
        }
    }

    // Check link tunnel_net
    for (link_pk, link) in links {
        let tunnel_ip = link.tunnel_net.ip();
        if !tunnel_ip.is_unspecified() && link.tunnel_net.prefix() > 0 {
            // Iterate over all IPs in the network (e.g., /31 has 2 IPs)
            for i in 0..link.tunnel_net.size() {
                if let Some(ip) = link.tunnel_net.nth(i) {
                    if !base_net.contains(ip) {
                        continue;
                    }
                    let ip_net = NetworkV4::new(ip, 32).unwrap();
                    let id_or_ip = IdOrIp::Ip(ip_net);
                    insert_usage(&mut in_use, id_or_ip, *link_pk, "Link".to_string());
                }
            }
            result.device_tunnel_block_checked += 1;
        }
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn verify_segment_routing_ids(
    program_id: &Pubkey,
    devices: &HashMap<Pubkey, Device>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::SegmentRoutingIds;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();

    for (device_pk, device) in devices {
        for iface in &device.interfaces {
            // Only check vpnv4/ipv4 loopbacks; node_segment_idx == 0 means
            // unallocated. flex_algo_node_segments is only populated on Vpnv4
            // loopbacks (one entry per topology) and pulls from the same
            // SegmentRoutingIds allocator, so it must be counted here too —
            // otherwise every flex-algo segment ID looks allocated-but-unused.
            if iface.interface_type == InterfaceType::Loopback
                && (iface.loopback_type == LoopbackType::Vpnv4
                    || iface.loopback_type == LoopbackType::Ipv4)
            {
                if iface.node_segment_idx != 0 {
                    insert_usage(
                        &mut in_use,
                        IdOrIp::Id(iface.node_segment_idx),
                        *device_pk,
                        format!("Device interface {}", iface.name),
                    );
                    result.segment_routing_ids_checked += 1;
                }
                for segment in &iface.flex_algo_node_segments {
                    if segment.node_segment_idx == 0 {
                        continue;
                    }
                    insert_usage(
                        &mut in_use,
                        IdOrIp::Id(segment.node_segment_idx),
                        *device_pk,
                        format!(
                            "Device interface {} flex-algo segment (topology {})",
                            iface.name, segment.topology
                        ),
                    );
                    result.segment_routing_ids_checked += 1;
                }
            }
        }
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn verify_link_ids(
    program_id: &Pubkey,
    links: &HashMap<Pubkey, Link>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::LinkIds;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();

    for (link_pk, link) in links {
        let id_or_ip = IdOrIp::Id(link.tunnel_id);
        insert_usage(&mut in_use, id_or_ip, *link_pk, "Link".to_string());
        result.link_ids_checked += 1;
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn verify_multicast_group_block(
    program_id: &Pubkey,
    multicast_groups: &HashMap<Pubkey, MulticastGroup>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::MulticastGroupBlock;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();

    for (group_pk, group) in multicast_groups {
        let ip = group.multicast_ip;
        if ip.is_multicast() {
            let ip_net = NetworkV4::new(ip, 32).unwrap();
            let id_or_ip = IdOrIp::Ip(ip_net);
            insert_usage(
                &mut in_use,
                id_or_ip,
                *group_pk,
                "MulticastGroup".to_string(),
            );
            result.multicast_group_block_checked += 1;
        }
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn verify_multicast_publisher_block(
    program_id: &Pubkey,
    users: &HashMap<Pubkey, User>,
    resource_extensions: &HashMap<Pubkey, ResourceExtensionOwned>,
    result: &mut VerifyResourceResult,
) {
    let resource_type = ResourceType::MulticastPublisherBlock;
    let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);

    let Some(extension) = resource_extensions.get(&pda) else {
        result
            .discrepancies
            .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
        return;
    };

    // Pull the base network so we can ignore legacy dz_ips that pre-date this
    // extension and fall outside the block's range.
    let base_net = match &extension.allocator {
        Allocator::Ip(ip_alloc) => ip_alloc.base_net,
        Allocator::Id(_) => return,
    };

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, Vec<(Pubkey, String)>> = HashMap::new();
    for (user_pk, user) in users {
        if user.user_type != UserType::Multicast || user.publishers.is_empty() {
            continue;
        }

        let dz_ip = user.dz_ip;
        if dz_ip.is_unspecified() || dz_ip == user.client_ip {
            continue;
        }

        if !base_net.contains(dz_ip) {
            continue;
        }

        let ip_net = NetworkV4::new(dz_ip, 32).unwrap();
        insert_usage(
            &mut in_use,
            IdOrIp::Ip(ip_net),
            *user_pk,
            "User".to_string(),
        );
        result.multicast_publisher_block_checked += 1;
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

/// Format a pubkey for display, prefixing the device/link code when known.
fn format_pubkey(pk: &Pubkey, labels: &HashMap<Pubkey, String>) -> String {
    match labels.get(pk) {
        Some(code) => format!("{} ({})", code, pk),
        None => pk.to_string(),
    }
}

/// Append a resource usage to the in_use map. Duplicates are detected later in
/// `check_discrepancies` by inspecting `accounts.len() >= 2`.
fn insert_usage(
    in_use: &mut HashMap<IdOrIp, Vec<(Pubkey, String)>>,
    value: IdOrIp,
    account_pubkey: Pubkey,
    account_type: String,
) {
    in_use
        .entry(value)
        .or_default()
        .push((account_pubkey, account_type));
}

fn check_discrepancies(
    resource_type: ResourceType,
    allocated: &HashSet<IdOrIp>,
    in_use: &HashMap<IdOrIp, Vec<(Pubkey, String)>>,
    result: &mut VerifyResourceResult,
) {
    // Find allocated but not used
    for alloc in allocated {
        if !in_use.contains_key(alloc) {
            result
                .discrepancies
                .push(ResourceDiscrepancy::AllocatedButNotUsed {
                    resource_type,
                    value: alloc.clone(),
                });
        }
    }

    // Emit one DuplicateUsage per value with multiple owners; otherwise check
    // used-but-not-allocated. Suppressing the second report avoids the same
    // value showing up under both sections.
    for (id_or_ip, owners) in in_use {
        if owners.len() >= 2 {
            result
                .discrepancies
                .push(ResourceDiscrepancy::DuplicateUsage {
                    resource_type,
                    value: id_or_ip.clone(),
                    accounts: owners.clone(),
                });
            continue;
        }
        if !allocated.contains(id_or_ip) {
            let (account_pk, account_type) = &owners[0];
            result
                .discrepancies
                .push(ResourceDiscrepancy::UsedButNotAllocated {
                    resource_type,
                    value: id_or_ip.clone(),
                    account_pubkey: *account_pk,
                    account_type: account_type.clone(),
                });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::{
        id_allocator::IdAllocator,
        ip_allocator::IpAllocator,
        state::resource_extension::{Allocator, ResourceExtensionOwned},
    };
    use std::io::Cursor;

    fn create_resource_extension_ip(
        program_id: &Pubkey,
        resource_type: ResourceType,
        base_ip: &str,
        storage: Vec<u8>,
    ) -> (Pubkey, ResourceExtensionOwned) {
        let (pda, bump, _) = get_resource_extension_pda(program_id, resource_type);
        let ip_net: NetworkV4 = base_ip.parse().unwrap();
        let allocator = IpAllocator::new(ip_net);
        (
            pda,
            ResourceExtensionOwned {
                account_type: AccountType::ResourceExtension,
                owner: *program_id,
                bump_seed: bump,
                associated_with: match resource_type {
                    ResourceType::DzPrefixBlock(pk, _) | ResourceType::TunnelIds(pk, _) => pk,
                    _ => Pubkey::default(),
                },
                allocator: Allocator::Ip(allocator),
                storage,
            },
        )
    }

    fn create_resource_extension_id(
        program_id: &Pubkey,
        resource_type: ResourceType,
        range: (u16, u16),
        storage: Vec<u8>,
    ) -> (Pubkey, ResourceExtensionOwned) {
        let (pda, bump, _) = get_resource_extension_pda(program_id, resource_type);
        let allocator = IdAllocator::new(range).unwrap();
        (
            pda,
            ResourceExtensionOwned {
                account_type: AccountType::ResourceExtension,
                owner: *program_id,
                bump_seed: bump,
                associated_with: match resource_type {
                    ResourceType::DzPrefixBlock(pk, _) | ResourceType::TunnelIds(pk, _) => pk,
                    _ => Pubkey::default(),
                },
                allocator: Allocator::Id(allocator),
                storage,
            },
        )
    }

    #[test]
    fn test_verify_no_discrepancies_when_empty() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Create empty resource extensions for all global types
        let user_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::UserTunnelBlock,
            "10.0.0.0/24",
            vec![0],
        );
        let device_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DeviceTunnelBlock,
            "172.16.0.0/24",
            vec![0],
        );
        let multicast_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastGroupBlock,
            "239.0.0.0/24",
            vec![0],
        );
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );
        let segment_routing = create_resource_extension_id(
            &program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );
        let link_ids =
            create_resource_extension_id(&program_id, ResourceType::LinkIds, (0, 100), vec![0; 13]);

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        accounts.insert(
            Box::new(user_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(user_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(device_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(device_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(multicast_block.0),
            Box::new(AccountData::ResourceExtension(multicast_block.1)),
        );
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );
        accounts.insert(
            Box::new(segment_routing.0),
            Box::new(AccountData::ResourceExtension(segment_routing.1)),
        );
        accounts.insert(
            Box::new(link_ids.0),
            Box::new(AccountData::ResourceExtension(link_ids.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(result.is_ok());
        assert_eq!(result.discrepancies.len(), 0);
    }

    #[test]
    fn test_verify_detects_allocated_but_not_used() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Create LinkIds extension with some allocations (first byte = 0xff means IDs 0-7 allocated)
        let link_ids = create_resource_extension_id(
            &program_id,
            ResourceType::LinkIds,
            (0, 100),
            vec![0xff; 1],
        );

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        // Add all required global resource extensions
        let user_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::UserTunnelBlock,
            "10.0.0.0/24",
            vec![0],
        );
        let device_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DeviceTunnelBlock,
            "172.16.0.0/24",
            vec![0],
        );
        let multicast_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastGroupBlock,
            "239.0.0.0/24",
            vec![0],
        );
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );
        let segment_routing = create_resource_extension_id(
            &program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );

        accounts.insert(
            Box::new(user_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(user_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(device_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(device_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(multicast_block.0),
            Box::new(AccountData::ResourceExtension(multicast_block.1)),
        );
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );
        accounts.insert(
            Box::new(segment_routing.0),
            Box::new(AccountData::ResourceExtension(segment_routing.1)),
        );
        accounts.insert(
            Box::new(link_ids.0),
            Box::new(AccountData::ResourceExtension(link_ids.1)),
        );
        // No links exist, so all allocated IDs should be orphaned

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(!result.is_ok());

        // Should have 8 allocated but not used discrepancies (IDs 0-7)
        let allocated_not_used: Vec<_> = result
            .discrepancies
            .iter()
            .filter(|d| matches!(d, ResourceDiscrepancy::AllocatedButNotUsed { .. }))
            .collect();
        assert_eq!(allocated_not_used.len(), 8);
    }

    #[test]
    fn test_verify_detects_extension_not_found() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Return empty accounts - no resource extensions exist
        let accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(!result.is_ok());

        // Should have ExtensionNotFound discrepancies for global resource types
        let extensions_not_found: Vec<_> = result
            .discrepancies
            .iter()
            .filter(|d| matches!(d, ResourceDiscrepancy::ExtensionNotFound { .. }))
            .collect();

        // Should find missing: UserTunnelBlock, DeviceTunnelBlock, MulticastGroupBlock,
        // MulticastPublisherBlock, SegmentRoutingIds, LinkIds
        assert!(extensions_not_found.len() >= 6);
    }

    #[test]
    fn test_output_format_no_discrepancies() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let user_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::UserTunnelBlock,
            "10.0.0.0/24",
            vec![0],
        );
        let device_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DeviceTunnelBlock,
            "172.16.0.0/24",
            vec![0],
        );
        let multicast_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastGroupBlock,
            "239.0.0.0/24",
            vec![0],
        );
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );
        let segment_routing = create_resource_extension_id(
            &program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );
        let link_ids =
            create_resource_extension_id(&program_id, ResourceType::LinkIds, (0, 100), vec![0; 13]);

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        accounts.insert(
            Box::new(user_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(user_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(device_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(device_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(multicast_block.0),
            Box::new(AccountData::ResourceExtension(multicast_block.1)),
        );
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );
        accounts.insert(
            Box::new(segment_routing.0),
            Box::new(AccountData::ResourceExtension(segment_routing.1)),
        );
        accounts.insert(
            Box::new(link_ids.0),
            Box::new(AccountData::ResourceExtension(link_ids.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let cmd = VerifyResourceCliCommand { fix: false };
        let mut output = Cursor::new(Vec::new());
        let result = cmd.execute(&mock_client, &mut output);
        assert!(result.is_ok());

        let output_str = String::from_utf8(output.into_inner()).unwrap();
        assert!(output_str.contains("Resource Verification Report"));
        assert!(output_str.contains("No discrepancies found."));
    }

    #[test]
    fn test_output_format_with_discrepancies() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Create LinkIds with allocations but no links
        let link_ids =
            create_resource_extension_id(&program_id, ResourceType::LinkIds, (0, 100), vec![0b11]); // IDs 0,1 allocated

        let user_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::UserTunnelBlock,
            "10.0.0.0/24",
            vec![0],
        );
        let device_tunnel_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DeviceTunnelBlock,
            "172.16.0.0/24",
            vec![0],
        );
        let multicast_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastGroupBlock,
            "239.0.0.0/24",
            vec![0],
        );
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );
        let segment_routing = create_resource_extension_id(
            &program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        accounts.insert(
            Box::new(user_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(user_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(device_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(device_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(multicast_block.0),
            Box::new(AccountData::ResourceExtension(multicast_block.1)),
        );
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );
        accounts.insert(
            Box::new(segment_routing.0),
            Box::new(AccountData::ResourceExtension(segment_routing.1)),
        );
        accounts.insert(
            Box::new(link_ids.0),
            Box::new(AccountData::ResourceExtension(link_ids.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let cmd = VerifyResourceCliCommand { fix: false };
        let mut output = Cursor::new(Vec::new());
        let result = cmd.execute(&mock_client, &mut output);
        assert!(result.is_ok());

        let output_str = String::from_utf8(output.into_inner()).unwrap();
        assert!(output_str.contains("Resource Verification Report"));
        assert!(output_str.contains("Discrepancies found:"));
        assert!(output_str.contains("Allocated but not used"));
        assert!(output_str.contains("LinkIds = 0"));
        assert!(output_str.contains("LinkIds = 1"));
    }

    fn make_publisher_user(
        device_pk: Pubkey,
        client_ip: [u8; 4],
        dz_ip: [u8; 4],
        publishers: Vec<Pubkey>,
    ) -> User {
        use doublezero_serviceability::state::user::{UserCYOA, UserStatus, UserType};
        User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: client_ip.into(),
            dz_ip: dz_ip.into(),
            tunnel_id: 0,
            tunnel_net: "0.0.0.0/0".parse().unwrap(),
            status: UserStatus::Activated,
            publishers,
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
        }
    }

    fn insert_global_ext_minimal(
        accounts: &mut HashMap<Box<Pubkey>, Box<AccountData>>,
        program_id: &Pubkey,
    ) {
        // Insert every global extension except MulticastPublisherBlock so tests
        // of that verifier don't get noise from other ExtensionNotFound entries.
        let user_tunnel_block = create_resource_extension_ip(
            program_id,
            ResourceType::UserTunnelBlock,
            "10.0.0.0/24",
            vec![0],
        );
        let device_tunnel_block = create_resource_extension_ip(
            program_id,
            ResourceType::DeviceTunnelBlock,
            "172.16.0.0/24",
            vec![0],
        );
        let multicast_block = create_resource_extension_ip(
            program_id,
            ResourceType::MulticastGroupBlock,
            "239.0.0.0/24",
            vec![0],
        );
        let segment_routing = create_resource_extension_id(
            program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );
        let link_ids =
            create_resource_extension_id(program_id, ResourceType::LinkIds, (0, 100), vec![0; 13]);

        accounts.insert(
            Box::new(user_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(user_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(device_tunnel_block.0),
            Box::new(AccountData::ResourceExtension(device_tunnel_block.1)),
        );
        accounts.insert(
            Box::new(multicast_block.0),
            Box::new(AccountData::ResourceExtension(multicast_block.1)),
        );
        accounts.insert(
            Box::new(segment_routing.0),
            Box::new(AccountData::ResourceExtension(segment_routing.1)),
        );
        accounts.insert(
            Box::new(link_ids.0),
            Box::new(AccountData::ResourceExtension(link_ids.1)),
        );
    }

    #[test]
    fn test_verify_multicast_publisher_block_happy_path() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // MulticastPublisherBlock with 148.51.120.5 allocated (bit 5 of byte 0).
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0x20],
        );

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_global_ext_minimal(&mut accounts, &program_id);
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );

        // A publisher user holding the allocated dz_ip.
        let publisher = make_publisher_user(
            Pubkey::new_unique(),
            [1, 2, 3, 4],
            [148, 51, 120, 5],
            vec![Pubkey::new_unique()],
        );
        let user_pk = Pubkey::new_unique();
        accounts.insert(Box::new(user_pk), Box::new(AccountData::User(publisher)));

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(
            result.is_ok(),
            "expected no discrepancies, got {:?}",
            result.discrepancies
        );
        assert_eq!(result.multicast_publisher_block_checked, 1);
    }

    #[test]
    fn test_verify_multicast_publisher_ignores_out_of_range_dz_ip() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Empty MulticastPublisherBlock.
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_global_ext_minimal(&mut accounts, &program_id);
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );

        // Legacy publisher with a dz_ip outside the block's range — must be ignored.
        let legacy_publisher = make_publisher_user(
            Pubkey::new_unique(),
            [1, 2, 3, 4],
            [10, 0, 0, 5],
            vec![Pubkey::new_unique()],
        );
        let legacy_pk = Pubkey::new_unique();
        accounts.insert(
            Box::new(legacy_pk),
            Box::new(AccountData::User(legacy_publisher)),
        );

        // Non-publisher Multicast user with a dz_ip in range — also must be ignored
        // (their dz_ip doesn't come from this block).
        let non_publisher = make_publisher_user(
            Pubkey::new_unique(),
            [1, 2, 3, 5],
            [148, 51, 120, 9],
            vec![],
        );
        let non_publisher_pk = Pubkey::new_unique();
        accounts.insert(
            Box::new(non_publisher_pk),
            Box::new(AccountData::User(non_publisher)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(
            result.is_ok(),
            "expected no discrepancies, got {:?}",
            result.discrepancies
        );
        assert_eq!(result.multicast_publisher_block_checked, 0);
    }

    #[test]
    fn test_verify_tunnel_ids_reports_missing_extension_for_device_without_users() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_global_ext_minimal(&mut accounts, &program_id);
        let multicast_publisher_block = create_resource_extension_ip(
            &program_id,
            ResourceType::MulticastPublisherBlock,
            "148.51.120.0/24",
            vec![0],
        );
        accounts.insert(
            Box::new(multicast_publisher_block.0),
            Box::new(AccountData::ResourceExtension(multicast_publisher_block.1)),
        );

        // Device with no users and no TunnelIds resource extension.
        let device_pk = Pubkey::new_unique();
        let device = doublezero_serviceability::state::device::Device::default();
        accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(
            result.discrepancies.iter().any(|d| matches!(
                d,
                ResourceDiscrepancy::ExtensionNotFound {
                    resource_type: ResourceType::TunnelIds(pk, 0),
                } if *pk == device_pk
            )),
            "expected ExtensionNotFound for TunnelIds of device with no users, got {:?}",
            result.discrepancies
        );
    }

    fn insert_all_globals(
        accounts: &mut HashMap<Box<Pubkey>, Box<AccountData>>,
        program_id: &Pubkey,
    ) {
        for ext in [
            create_resource_extension_ip(
                program_id,
                ResourceType::UserTunnelBlock,
                "10.0.0.0/24",
                vec![0],
            ),
            create_resource_extension_ip(
                program_id,
                ResourceType::DeviceTunnelBlock,
                "172.16.0.0/24",
                vec![0],
            ),
            create_resource_extension_ip(
                program_id,
                ResourceType::MulticastGroupBlock,
                "239.0.0.0/24",
                vec![0],
            ),
            create_resource_extension_ip(
                program_id,
                ResourceType::MulticastPublisherBlock,
                "148.51.120.0/24",
                vec![0],
            ),
            create_resource_extension_id(
                program_id,
                ResourceType::SegmentRoutingIds,
                (0, 100),
                vec![0; 13],
            ),
            create_resource_extension_id(program_id, ResourceType::LinkIds, (0, 100), vec![0; 13]),
        ] {
            accounts.insert(
                Box::new(ext.0),
                Box::new(AccountData::ResourceExtension(ext.1)),
            );
        }
    }

    #[test]
    fn test_orphaned_extension_from_deleted_device() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);

        // Simulate a TunnelIds extension for a device that no longer exists.
        let dead_device_pk = Pubkey::new_unique();
        let orphan_tunnel_ids = create_resource_extension_id(
            &program_id,
            ResourceType::TunnelIds(dead_device_pk, 0),
            (0, 100),
            vec![0; 13],
        );
        let orphan_pda = orphan_tunnel_ids.0;
        accounts.insert(
            Box::new(orphan_tunnel_ids.0),
            Box::new(AccountData::ResourceExtension(orphan_tunnel_ids.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();

        let orphans: Vec<_> = result
            .discrepancies
            .iter()
            .filter_map(|d| match d {
                ResourceDiscrepancy::OrphanedExtension {
                    pubkey,
                    associated_with,
                    ..
                } => Some((*pubkey, *associated_with)),
                _ => None,
            })
            .collect();
        assert_eq!(orphans.len(), 1);
        assert_eq!(orphans[0].0, orphan_pda);
        assert_eq!(orphans[0].1, dead_device_pk);
    }

    #[test]
    fn test_orphaned_extension_from_stale_dz_prefix() {
        use doublezero_serviceability::state::device::Device;

        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);

        // Create a live device with a single dz_prefix (index 0).
        let device_pk = Pubkey::new_unique();
        let prefix_net: NetworkV4 = "10.1.0.0/24".parse().unwrap();
        let device = Device {
            dz_prefixes: vec![prefix_net].into(),
            ..Device::default()
        };
        accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));

        // Legitimate DzPrefixBlock for index 0. First IP reserved for the device.
        let live_prefix_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DzPrefixBlock(device_pk, 0),
            "10.1.0.0/24",
            vec![0x01], // first IP allocated (reserved for device)
        );
        accounts.insert(
            Box::new(live_prefix_block.0),
            Box::new(AccountData::ResourceExtension(live_prefix_block.1)),
        );
        // Legitimate TunnelIds for the device.
        let live_tunnel_ids = create_resource_extension_id(
            &program_id,
            ResourceType::TunnelIds(device_pk, 0),
            (0, 100),
            vec![0; 13],
        );
        accounts.insert(
            Box::new(live_tunnel_ids.0),
            Box::new(AccountData::ResourceExtension(live_tunnel_ids.1)),
        );

        // Stale DzPrefixBlock at index 5 — the device no longer has that prefix.
        let stale_prefix_block = create_resource_extension_ip(
            &program_id,
            ResourceType::DzPrefixBlock(device_pk, 5),
            "10.9.0.0/24",
            vec![0],
        );
        let stale_pda = stale_prefix_block.0;
        accounts.insert(
            Box::new(stale_prefix_block.0),
            Box::new(AccountData::ResourceExtension(stale_prefix_block.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();

        let orphan_pdas: Vec<Pubkey> = result
            .discrepancies
            .iter()
            .filter_map(|d| match d {
                ResourceDiscrepancy::OrphanedExtension { pubkey, .. } => Some(*pubkey),
                _ => None,
            })
            .collect();
        assert_eq!(orphan_pdas, vec![stale_pda]);
    }

    #[test]
    fn test_vrf_ids_and_admin_group_bits_not_flagged_as_orphans() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);

        // VrfIds and AdminGroupBits aren't verified against usage but must be
        // recognized as legitimate global singletons.
        let vrf_ids =
            create_resource_extension_id(&program_id, ResourceType::VrfIds, (0, 100), vec![0; 13]);
        accounts.insert(
            Box::new(vrf_ids.0),
            Box::new(AccountData::ResourceExtension(vrf_ids.1)),
        );
        let admin_group_bits = create_resource_extension_id(
            &program_id,
            ResourceType::AdminGroupBits,
            (0, 64),
            vec![0; 8],
        );
        accounts.insert(
            Box::new(admin_group_bits.0),
            Box::new(AccountData::ResourceExtension(admin_group_bits.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        assert!(
            !result
                .discrepancies
                .iter()
                .any(|d| matches!(d, ResourceDiscrepancy::OrphanedExtension { .. })),
            "VrfIds/AdminGroupBits should not be flagged as orphans: {:?}",
            result.discrepancies
        );
    }

    #[test]
    fn test_output_includes_orphan_section() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);

        let dead_device_pk = Pubkey::new_unique();
        let orphan = create_resource_extension_id(
            &program_id,
            ResourceType::TunnelIds(dead_device_pk, 0),
            (0, 100),
            vec![0; 13],
        );
        let orphan_pda = orphan.0;
        accounts.insert(
            Box::new(orphan.0),
            Box::new(AccountData::ResourceExtension(orphan.1)),
        );

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let cmd = VerifyResourceCliCommand { fix: false };
        let mut output = Cursor::new(Vec::new());
        cmd.execute(&mock_client, &mut output).unwrap();
        let output_str = String::from_utf8(output.into_inner()).unwrap();
        assert!(output_str.contains("Orphaned resource extensions"));
        assert!(output_str.contains(&orphan_pda.to_string()));
        assert!(output_str.contains(&dead_device_pk.to_string()));
        assert!(output_str.contains("Hint: use --fix to close orphaned resource extensions."));
    }

    fn make_segment_routing_ext(
        program_id: &Pubkey,
        allocated_ids: &[u16],
    ) -> (Pubkey, ResourceExtensionOwned) {
        let (pda, mut ext) = create_resource_extension_id(
            program_id,
            ResourceType::SegmentRoutingIds,
            (0, 100),
            vec![0; 13],
        );
        if let Allocator::Id(ref mut a) = ext.allocator {
            for id in allocated_ids {
                a.allocate_specific(&mut ext.storage, *id).unwrap();
            }
        } else {
            panic!("expected Id allocator");
        }
        (pda, ext)
    }

    fn make_vpnv4_loopback(
        name: &str,
        node_segment_idx: u16,
        flex_algo_segments: Vec<doublezero_serviceability::state::topology::FlexAlgoNodeSegment>,
    ) -> doublezero_serviceability::state::interface::Interface {
        use doublezero_serviceability::state::interface::{
            Interface, InterfaceStatus, InterfaceType, LoopbackType,
        };
        Interface {
            status: InterfaceStatus::Activated,
            name: name.to_string(),
            interface_type: InterfaceType::Loopback,
            loopback_type: LoopbackType::Vpnv4,
            node_segment_idx,
            flex_algo_node_segments: flex_algo_segments,
            ..Interface::default()
        }
    }

    fn make_vpnv4_loopback_with_ip(
        name: &str,
        ip_net: &str,
    ) -> doublezero_serviceability::state::interface::Interface {
        let mut iface = make_vpnv4_loopback(name, 0, vec![]);
        iface.ip_net = ip_net.parse().unwrap();
        iface
    }

    fn segment_routing_discrepancies(result: &VerifyResourceResult) -> Vec<&ResourceDiscrepancy> {
        result
            .discrepancies
            .iter()
            .filter(|d| match d {
                ResourceDiscrepancy::AllocatedButNotUsed { resource_type, .. }
                | ResourceDiscrepancy::UsedButNotAllocated { resource_type, .. }
                | ResourceDiscrepancy::DuplicateUsage { resource_type, .. } => {
                    matches!(resource_type, ResourceType::SegmentRoutingIds)
                }
                _ => false,
            })
            .collect()
    }

    #[test]
    fn test_verify_segment_routing_ids_counts_flex_algo_segments() {
        use doublezero_serviceability::state::topology::FlexAlgoNodeSegment;

        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Override the default SegmentRoutingIds extension to mark IDs 7 and 8
        // as allocated. 7 is the loopback's base segment, 8 is the flex-algo
        // segment for one topology.
        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);
        let sr = make_segment_routing_ext(&program_id, &[7, 8]);
        accounts.insert(
            Box::new(sr.0),
            Box::new(AccountData::ResourceExtension(sr.1)),
        );

        let device_pk = Pubkey::new_unique();
        let topology_pk = Pubkey::new_unique();
        let device = Device {
            interfaces: vec![make_vpnv4_loopback(
                "Loopback0",
                7,
                vec![FlexAlgoNodeSegment {
                    topology: topology_pk,
                    node_segment_idx: 8,
                }],
            )],
            ..Device::default()
        };
        accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        let discrepancies = segment_routing_discrepancies(&result);
        assert!(
            discrepancies.is_empty(),
            "expected no SegmentRoutingIds discrepancies, got {:?}",
            discrepancies
        );
        // Both the base segment and the flex-algo segment should be counted.
        assert_eq!(result.segment_routing_ids_checked, 2);
    }

    #[test]
    fn test_verify_segment_routing_ids_flex_algo_used_but_not_allocated() {
        use doublezero_serviceability::state::topology::FlexAlgoNodeSegment;

        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Allocator has only the base segment (7), not the flex-algo one (8).
        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);
        let sr = make_segment_routing_ext(&program_id, &[7]);
        accounts.insert(
            Box::new(sr.0),
            Box::new(AccountData::ResourceExtension(sr.1)),
        );

        let device_pk = Pubkey::new_unique();
        let topology_pk = Pubkey::new_unique();
        let device = Device {
            interfaces: vec![make_vpnv4_loopback(
                "Loopback0",
                7,
                vec![FlexAlgoNodeSegment {
                    topology: topology_pk,
                    node_segment_idx: 8,
                }],
            )],
            ..Device::default()
        };
        accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        let discrepancies = segment_routing_discrepancies(&result);
        assert_eq!(discrepancies.len(), 1, "got {:?}", discrepancies);
        match discrepancies[0] {
            ResourceDiscrepancy::UsedButNotAllocated {
                value,
                account_type,
                ..
            } => {
                assert_eq!(*value, IdOrIp::Id(8));
                assert!(
                    account_type.contains("flex-algo"),
                    "account_type should mention flex-algo: {}",
                    account_type
                );
                assert!(account_type.contains(&topology_pk.to_string()));
            }
            other => panic!("unexpected discrepancy: {:?}", other),
        }
    }

    #[test]
    fn test_duplicate_usage_not_double_reported_as_used_but_not_allocated() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Allocator has nothing — the shared ID is used by two devices but
        // not allocated. We want exactly one DuplicateUsage and zero
        // UsedButNotAllocated for that value.
        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);
        let sr = make_segment_routing_ext(&program_id, &[]);
        accounts.insert(
            Box::new(sr.0),
            Box::new(AccountData::ResourceExtension(sr.1)),
        );

        let dev_a = Pubkey::new_unique();
        let dev_b = Pubkey::new_unique();
        for pk in [dev_a, dev_b] {
            let device = Device {
                interfaces: vec![make_vpnv4_loopback("Loopback0", 42, vec![])],
                ..Device::default()
            };
            accounts.insert(Box::new(pk), Box::new(AccountData::Device(device)));
        }

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        let discrepancies = segment_routing_discrepancies(&result);

        let dup_count = discrepancies
            .iter()
            .filter(|d| {
                matches!(
                    d,
                    ResourceDiscrepancy::DuplicateUsage {
                        value: IdOrIp::Id(42),
                        ..
                    }
                )
            })
            .count();
        let used_not_alloc_count = discrepancies
            .iter()
            .filter(|d| {
                matches!(
                    d,
                    ResourceDiscrepancy::UsedButNotAllocated {
                        value: IdOrIp::Id(42),
                        ..
                    }
                )
            })
            .count();
        assert_eq!(dup_count, 1, "discrepancies: {:?}", discrepancies);
        assert_eq!(
            used_not_alloc_count, 0,
            "discrepancies: {:?}",
            discrepancies
        );

        // And the single DuplicateUsage should list both owners.
        let dup = discrepancies
            .iter()
            .find_map(|d| match d {
                ResourceDiscrepancy::DuplicateUsage { accounts, .. } => Some(accounts),
                _ => None,
            })
            .expect("DuplicateUsage present");
        assert_eq!(dup.len(), 2);
    }

    #[test]
    fn test_duplicate_usage_with_three_owners_emits_single_entry() {
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        // Allocator has the ID, so the only expected discrepancy is the
        // duplicate report.
        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);
        let sr = make_segment_routing_ext(&program_id, &[42]);
        accounts.insert(
            Box::new(sr.0),
            Box::new(AccountData::ResourceExtension(sr.1)),
        );

        for _ in 0..3 {
            let device_pk = Pubkey::new_unique();
            let device = Device {
                interfaces: vec![make_vpnv4_loopback("Loopback0", 42, vec![])],
                ..Device::default()
            };
            accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));
        }

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        let dup_entries: Vec<&Vec<(Pubkey, String)>> = result
            .discrepancies
            .iter()
            .filter_map(|d| match d {
                ResourceDiscrepancy::DuplicateUsage {
                    resource_type: ResourceType::SegmentRoutingIds,
                    value: IdOrIp::Id(42),
                    accounts,
                } => Some(accounts),
                _ => None,
            })
            .collect();
        assert_eq!(
            dup_entries.len(),
            1,
            "want one DuplicateUsage, got {:?}",
            dup_entries
        );
        assert_eq!(dup_entries[0].len(), 3);
    }

    #[test]
    fn test_output_includes_device_and_link_codes() {
        // A duplicate-usage on a SegmentRoutingId across two devices with
        // codes "dz1" and "dz2" should show both codes alongside their
        // pubkeys in the rendered "Duplicate usage" section.
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);
        let sr = make_segment_routing_ext(&program_id, &[42]);
        accounts.insert(
            Box::new(sr.0),
            Box::new(AccountData::ResourceExtension(sr.1)),
        );

        let dev_a_pk = Pubkey::new_unique();
        let dev_b_pk = Pubkey::new_unique();
        for (pk, code) in [(dev_a_pk, "dz1"), (dev_b_pk, "dz2")] {
            let device = Device {
                code: code.to_string(),
                interfaces: vec![make_vpnv4_loopback("Loopback0", 42, vec![])],
                ..Device::default()
            };
            accounts.insert(Box::new(pk), Box::new(AccountData::Device(device)));
        }

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let cmd = VerifyResourceCliCommand { fix: false };
        let mut output = Cursor::new(Vec::new());
        cmd.execute(&mock_client, &mut output).unwrap();
        let output_str = String::from_utf8(output.into_inner()).unwrap();

        assert!(
            output_str.contains(&format!("dz1 ({})", dev_a_pk)),
            "expected `dz1 ({})` in output, got:\n{}",
            dev_a_pk,
            output_str
        );
        assert!(
            output_str.contains(&format!("dz2 ({})", dev_b_pk)),
            "expected `dz2 ({})` in output, got:\n{}",
            dev_b_pk,
            output_str
        );
    }

    #[test]
    fn test_verify_device_tunnel_block_ignores_loopback_ip_outside_base_net() {
        // User-tunnel-endpoint Vpnv4 loopbacks land on a caller-supplied
        // globally routable IP and skip the DeviceTunnelBlock allocator
        // (see processors/device/interface/create.rs). The verifier must
        // not report those IPs as `UsedButNotAllocated`.
        let mut mock_client = MockCliCommand::new();
        let program_id = Pubkey::new_unique();

        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        insert_all_globals(&mut accounts, &program_id);

        // The DeviceTunnelBlock from insert_all_globals is 172.16.0.0/24.
        // Give the loopback a globally routable IP outside that block.
        let device_pk = Pubkey::new_unique();
        let device = Device {
            interfaces: vec![make_vpnv4_loopback_with_ip("Loopback0", "203.0.113.5/32")],
            ..Device::default()
        };
        accounts.insert(Box::new(device_pk), Box::new(AccountData::Device(device)));

        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        mock_client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let result = verify_resources(&mock_client).unwrap();
        let dtb_used_not_alloc: Vec<_> = result
            .discrepancies
            .iter()
            .filter(|d| {
                matches!(
                    d,
                    ResourceDiscrepancy::UsedButNotAllocated {
                        resource_type: ResourceType::DeviceTunnelBlock,
                        ..
                    }
                )
            })
            .collect();
        assert!(
            dtb_used_not_alloc.is_empty(),
            "loopback IP outside the DeviceTunnelBlock base_net should not produce a UsedButNotAllocated entry, got {:?}",
            dtb_used_not_alloc
        );
    }
}
