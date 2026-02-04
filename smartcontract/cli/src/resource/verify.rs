use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::commands::resource::{
    allocate::AllocateResourceCommand, deallocate::DeallocateResourceCommand,
};
use doublezero_serviceability::{
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    state::{
        accountdata::AccountData, device::Device, interface::InterfaceType, link::Link,
        multicastgroup::MulticastGroup, resource_extension::ResourceExtensionOwned, user::User,
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
                            resource_type, value, account_type, account_pubkey
                        )?;
                    }
                }
                writeln!(out)?;
            }

            // Handle --fix flag
            if self.fix && !extensions_not_found.is_empty() {
                writeln!(
                    out,
                    "Cannot fix: some resource extensions are missing. Create them first."
                )?;
            } else if self.fix && (!allocated_not_used.is_empty() || !used_not_allocated.is_empty())
            {
                writeln!(out, "Proposed fixes:")?;
                writeln!(out, "--------------")?;

                for d in &allocated_not_used {
                    if let ResourceDiscrepancy::AllocatedButNotUsed {
                        resource_type,
                        value,
                    } = d
                    {
                        writeln!(out, "  DEALLOCATE {} = {}", resource_type, value)?;
                    }
                }

                for d in &used_not_allocated {
                    if let ResourceDiscrepancy::UsedButNotAllocated {
                        resource_type,
                        value,
                        ..
                    } = d
                    {
                        writeln!(out, "  ALLOCATE {} = {}", resource_type, value)?;
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
                    for d in &allocated_not_used {
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
                    for d in &used_not_allocated {
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

                    writeln!(out)?;
                    writeln!(out, "Done.")?;
                } else {
                    writeln!(out, "Aborted.")?;
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

    Ok(result)
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
    let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();
    for (user_pk, user) in users {
        let tunnel_ip = user.tunnel_net.ip();
        if !tunnel_ip.is_unspecified() && user.tunnel_net.prefix() > 0 {
            // Iterate over all IPs in the network (e.g., /31 has 2 IPs)
            for i in 0..user.tunnel_net.size() {
                if let Some(ip) = user.tunnel_net.nth(i) {
                    let ip_net = NetworkV4::new(ip, 32).unwrap();
                    let id_or_ip = IdOrIp::Ip(ip_net);
                    in_use.insert(id_or_ip, (*user_pk, "User".to_string()));
                }
            }
            result.user_tunnel_block_checked += 1;
        }
    }

    // Check for discrepancies
    check_discrepancies(ResourceType::UserTunnelBlock, &allocated, &in_use, result);
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
            // Only report if this device has users
            if users_by_device.contains_key(device_pk) {
                result
                    .discrepancies
                    .push(ResourceDiscrepancy::ExtensionNotFound { resource_type });
            }
            continue;
        };

        let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

        let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();
        if let Some(device_users) = users_by_device.get(device_pk) {
            for (user_pk, user) in device_users {
                if user.tunnel_id != 0 {
                    let id_or_ip = IdOrIp::Id(user.tunnel_id);
                    in_use.insert(id_or_ip, (*user_pk, "User".to_string()));
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
            let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();

            // First IP is reserved for the device itself (Loopback100)
            let first_ip = prefix.ip();
            let first_ip_net = NetworkV4::new(first_ip, 32).unwrap();
            in_use.insert(
                IdOrIp::Ip(first_ip_net),
                (*device_pk, "Device (reserved)".to_string()),
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
                    in_use.insert(id_or_ip, (*user_pk, "User".to_string()));
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

    let allocated: HashSet<IdOrIp> = extension.iter_allocated().into_iter().collect();

    let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();

    // Check device loopback interfaces
    for (device_pk, device) in devices {
        for interface in &device.interfaces {
            let iface = interface.into_current_version();
            if iface.interface_type == InterfaceType::Loopback {
                let ip = iface.ip_net.ip();
                if !ip.is_unspecified() && iface.ip_net.prefix() > 0 {
                    // Iterate over all IPs in the network
                    for i in 0..iface.ip_net.size() {
                        if let Some(ip) = iface.ip_net.nth(i) {
                            let ip_net = NetworkV4::new(ip, 32).unwrap();
                            let id_or_ip = IdOrIp::Ip(ip_net);
                            in_use.insert(
                                id_or_ip,
                                (*device_pk, format!("Device interface {}", iface.name)),
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
                    let ip_net = NetworkV4::new(ip, 32).unwrap();
                    let id_or_ip = IdOrIp::Ip(ip_net);
                    in_use.insert(id_or_ip, (*link_pk, "Link".to_string()));
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

    let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();

    for (device_pk, device) in devices {
        for interface in &device.interfaces {
            let iface = interface.into_current_version();
            // node_segment_idx == 0 means not allocated
            if iface.node_segment_idx != 0 {
                let id_or_ip = IdOrIp::Id(iface.node_segment_idx);
                in_use.insert(
                    id_or_ip,
                    (*device_pk, format!("Device interface {}", iface.name)),
                );
                result.segment_routing_ids_checked += 1;
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

    let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();

    for (link_pk, link) in links {
        let id_or_ip = IdOrIp::Id(link.tunnel_id);
        in_use.insert(id_or_ip, (*link_pk, "Link".to_string()));
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

    let mut in_use: HashMap<IdOrIp, (Pubkey, String)> = HashMap::new();

    for (group_pk, group) in multicast_groups {
        let ip = group.multicast_ip;
        if ip.is_multicast() {
            let ip_net = NetworkV4::new(ip, 32).unwrap();
            let id_or_ip = IdOrIp::Ip(ip_net);
            in_use.insert(id_or_ip, (*group_pk, "MulticastGroup".to_string()));
            result.multicast_group_block_checked += 1;
        }
    }

    check_discrepancies(resource_type, &allocated, &in_use, result);
}

fn check_discrepancies(
    resource_type: ResourceType,
    allocated: &HashSet<IdOrIp>,
    in_use: &HashMap<IdOrIp, (Pubkey, String)>,
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

    // Find used but not allocated
    for (id_or_ip, (account_pk, account_type)) in in_use {
        if !allocated.contains(id_or_ip) {
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

        // Should find missing: UserTunnelBlock, DeviceTunnelBlock, MulticastGroupBlock, SegmentRoutingIds, LinkIds
        assert!(extensions_not_found.len() >= 5);
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
}
