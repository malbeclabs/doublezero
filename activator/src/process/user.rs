use crate::{
    activator_metrics::record_device_ip_metrics, idallocator::IDAllocator,
    ipblockallocator::IPBlockAllocator, processor::DeviceMap, states::devicestate::DeviceState,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand,
        user::{
            activate::ActivateUserCommand, ban::BanUserCommand,
            closeaccount::CloseAccountUserCommand, reject::RejectUserCommand,
        },
    },
    DoubleZeroClient, Exchange, Location, User, UserStatus, UserType,
};
use doublezero_serviceability::error::DoubleZeroError;
use log::{info, warn};
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::{
    collections::hash_map::{Entry, HashMap},
    fmt::Write,
    net::Ipv4Addr,
};

#[allow(clippy::too_many_arguments)]
pub fn process_user_event(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    devices: &mut DeviceMap,
    user_tunnel_ips: &mut IPBlockAllocator,
    publisher_dz_ips: &mut Option<IPBlockAllocator>,
    link_ids: &mut IDAllocator,
    user: &User,
    locations: &HashMap<Pubkey, Location>,
    exchanges: &HashMap<Pubkey, Exchange>,
    use_onchain_allocation: bool,
) {
    match user.status {
        // Create User
        UserStatus::Pending => {
            let mut log_msg = String::new();
            write!(
                &mut log_msg,
                "Event:User(Pending) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            let device_state = match get_or_insert_device_state(client, pubkey, devices, user) {
                Some(ds) => ds,
                None => {
                    // Reject user since we couldn't get their user block
                    let res = reject_user(client, pubkey, "Error: Device not found");

                    match res {
                        Ok(signature) => {
                            write!(
                                &mut log_msg,
                                " Reject(Device not found) Rejected {signature}"
                            )
                            .unwrap();
                        }
                        Err(e) => {
                            write!(&mut log_msg, " Reject(Device not found) Error: {e}").unwrap();
                        }
                    }
                    info!("{log_msg}");
                    return;
                }
            };

            write!(&mut log_msg, " for: {}", device_state.device.code).unwrap();

            // Try to get tunnel network
            let tunnel_net = match user_tunnel_ips.next_available_block(0, 2) {
                Some(net) => net,
                None => {
                    // Reject user since we couldn't get their user block
                    let res = reject_user(client, pubkey, "Error: No available user block");

                    match res {
                        Ok(signature) => {
                            write!(
                                &mut log_msg,
                                " Reject(No available user block) Rejected {signature}"
                            )
                            .unwrap();
                        }
                        Err(e) => {
                            write!(&mut log_msg, " Reject(No available user block) Error: {e}")
                                .unwrap();
                        }
                    }
                    info!("{log_msg}");
                    return;
                }
            };

            write!(&mut log_msg, " tunnel_net: {} ", &tunnel_net).unwrap();

            let tunnel_id = device_state.get_next_tunnel_id();

            // Determine tunnel endpoint: if the client demanded a specific one, validate it;
            // otherwise fall back to first-available (backwards compat with 0.0.0.0).
            let tunnel_endpoint =
                match resolve_tunnel_endpoint(client, pubkey, device_state, user, &mut log_msg) {
                    Some(ep) => ep,
                    None => return,
                };

            let need_dz_ip = user.needs_allocated_dz_ip();

            // Determine allocation strategy for dz_ip:
            // - Multicast publishers: use publisher_dz_ips pool if available, otherwise onchain
            // - Other types: respect use_onchain_allocation flag
            let is_publisher = user.user_type == UserType::Multicast && !user.publishers.is_empty();
            let use_onchain_dz_ip = if is_publisher {
                use_onchain_allocation || publisher_dz_ips.is_none()
            } else {
                use_onchain_allocation
            };

            let dz_ip = if need_dz_ip && !use_onchain_dz_ip {
                // Offchain allocation
                if is_publisher {
                    // Publishers: allocate from global publisher pool
                    if let Some(ref mut publisher_ips) = publisher_dz_ips {
                        match publisher_ips.next_available_block(1, 1).map(|net| net.ip()) {
                            Some(ip) => ip,
                            None => {
                                let res = reject_user(
                                    client,
                                    pubkey,
                                    "Error: No available publisher dz_ip to allocate",
                                );

                                match res {
                                    Ok(signature) => {
                                        write!(
                                            &mut log_msg,
                                            " Reject(No available publisher dz_ip) Rejected {signature}"
                                        )
                                        .unwrap();
                                    }
                                    Err(e) => {
                                        write!(
                                            &mut log_msg,
                                            " Reject(No available publisher dz_ip) Error: {e}"
                                        )
                                        .unwrap();
                                    }
                                }
                                info!("{log_msg}");
                                return;
                            }
                        }
                    } else {
                        // Should never happen due to use_onchain_dz_ip check above
                        let res =
                            reject_user(client, pubkey, "Error: Publisher dz_ip pool unavailable");

                        match res {
                            Ok(signature) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(Publisher dz_ip pool unavailable) Rejected {signature}"
                                )
                                .unwrap();
                            }
                            Err(e) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(Publisher dz_ip pool unavailable) Error: {e}"
                                )
                                .unwrap();
                            }
                        }
                        info!("{log_msg}");
                        return;
                    }
                } else {
                    // IBRL/EdgeFiltering: allocate from device state
                    match device_state.get_next_dz_ip() {
                        Some(ip) => ip,
                        None => {
                            let res = reject_user(
                                client,
                                pubkey,
                                "Error: No available dz_ip to allocate",
                            );

                            match res {
                                Ok(signature) => {
                                    write!(
                                        &mut log_msg,
                                        " Reject(No available dz_ip to allocate) Rejected {signature}"
                                    )
                                    .unwrap();
                                }
                                Err(e) => {
                                    write!(
                                        &mut log_msg,
                                        " Reject(No available dz_ip to allocate) Error: {e}"
                                    )
                                    .unwrap();
                                }
                            }
                            info!("{log_msg}");
                            return;
                        }
                    }
                }
            } else if need_dz_ip {
                // Onchain allocation: pass UNSPECIFIED so smart contract allocates
                Ipv4Addr::UNSPECIFIED
            } else {
                user.client_ip
            };

            write!(&mut log_msg, " tunnel_id: {} dz_ip: {} ", tunnel_id, &dz_ip).unwrap();

            // Activate the user
            // Force onchain allocation for multicast publishers if no local publisher pool
            let use_onchain_for_activation =
                use_onchain_allocation || (is_publisher && publisher_dz_ips.is_none());

            let res = ActivateUserCommand {
                user_pubkey: *pubkey,
                tunnel_id: if use_onchain_for_activation {
                    0
                } else {
                    tunnel_id
                },
                tunnel_net: if use_onchain_for_activation {
                    NetworkV4::default()
                } else {
                    tunnel_net.into()
                },
                dz_ip,
                use_onchain_allocation: use_onchain_for_activation,
                tunnel_endpoint,
            }
            .execute(client);

            match res {
                Ok(signature) => {
                    let suffix = if use_onchain_allocation {
                        " (on-chain)"
                    } else {
                        ""
                    };
                    write!(&mut log_msg, " Activated{suffix}   {signature}").unwrap();
                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "user-pending-to-activated",
                        "user-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                    // Register the tunnel endpoint as in use for this client
                    device_state.register_tunnel_endpoint(user.client_ip, tunnel_endpoint);
                    record_device_ip_metrics(&user.device_pk, device_state, locations, exchanges);
                }
                Err(e) => {
                    log_error_ignore_invalid_status(&mut log_msg, e);
                }
            }

            info!("{log_msg}");
        }
        UserStatus::Updating => {
            let mut log_msg = String::new();

            write!(
                &mut log_msg,
                "Event:User(Updating) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            let device_state = match get_or_insert_device_state(client, pubkey, devices, user) {
                Some(ds) => ds,
                None => return,
            };

            write!(
                &mut log_msg,
                " Activating User: {}, for: {}",
                &user.client_ip, device_state.device.code
            )
            .unwrap();

            let need_dz_ip = user.needs_allocated_dz_ip();

            let is_publisher = user.user_type == UserType::Multicast && !user.publishers.is_empty();
            let use_onchain_dz_ip = if is_publisher {
                use_onchain_allocation || publisher_dz_ips.is_none()
            } else {
                use_onchain_allocation
            };

            let dz_ip = if need_dz_ip && user.dz_ip == user.client_ip {
                if use_onchain_dz_ip {
                    // Onchain allocation: pass UNSPECIFIED so smart contract allocates
                    Ipv4Addr::UNSPECIFIED
                } else if is_publisher {
                    // Publishers: allocate from global publisher pool
                    if let Some(ref mut publisher_ips) = publisher_dz_ips {
                        match publisher_ips.next_available_block(1, 1).map(|net| net.ip()) {
                            Some(ip) => ip,
                            None => {
                                let res = reject_user(
                                    client,
                                    pubkey,
                                    "Error: No available publisher dz_ip to allocate",
                                );

                                match res {
                                    Ok(signature) => {
                                        write!(
                                            &mut log_msg,
                                            " Reject(No available publisher dz_ip) Rejected {signature}"
                                        )
                                        .unwrap();
                                    }
                                    Err(e) => {
                                        write!(
                                            &mut log_msg,
                                            " Reject(No available publisher dz_ip) Error: {e}"
                                        )
                                        .unwrap();
                                    }
                                }
                                info!("{log_msg}");
                                return;
                            }
                        }
                    } else {
                        // Should never happen due to use_onchain_dz_ip check above
                        let res =
                            reject_user(client, pubkey, "Error: Publisher dz_ip pool unavailable");

                        match res {
                            Ok(signature) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(Publisher dz_ip pool unavailable) Rejected {signature}"
                                )
                                .unwrap();
                            }
                            Err(e) => {
                                write!(
                                    &mut log_msg,
                                    " Reject(Publisher dz_ip pool unavailable) Error: {e}"
                                )
                                .unwrap();
                            }
                        }
                        info!("{log_msg}");
                        return;
                    }
                } else {
                    // Non-publisher: allocate from device state
                    match device_state.get_next_dz_ip() {
                        Some(ip) => ip,
                        None => {
                            let res = reject_user(
                                client,
                                pubkey,
                                "Error: No available dz_ip to allocate",
                            );

                            match res {
                                Ok(signature) => {
                                    write!(
                                        &mut log_msg,
                                        " Reject(No available dz_ip) Rejected {signature}"
                                    )
                                    .unwrap();
                                }
                                Err(e) => {
                                    write!(&mut log_msg, " Reject(No available dz_ip) Error: {e}")
                                        .unwrap();
                                }
                            }
                            info!("{log_msg}");
                            return;
                        }
                    }
                }
            } else {
                user.dz_ip
            };

            write!(
                &mut log_msg,
                " tunnel_net: {} tunnel_id: {} dz_ip: {} ",
                &user.tunnel_net, user.tunnel_id, &dz_ip
            )
            .unwrap();

            // Determine tunnel endpoint: if the client demanded a specific one, validate it;
            // otherwise fall back to first-available (backwards compat with 0.0.0.0).
            let tunnel_endpoint =
                match resolve_tunnel_endpoint(client, pubkey, device_state, user, &mut log_msg) {
                    Some(ep) => ep,
                    None => return,
                };

            // Activate the user
            let use_onchain_for_activation =
                use_onchain_allocation || (is_publisher && publisher_dz_ips.is_none());

            let res = ActivateUserCommand {
                user_pubkey: *pubkey,
                tunnel_id: user.tunnel_id,
                tunnel_net: user.tunnel_net,
                dz_ip,
                use_onchain_allocation: use_onchain_for_activation,
                tunnel_endpoint,
            }
            .execute(client);
            match res {
                Ok(signature) => {
                    let suffix = if use_onchain_allocation {
                        " (on-chain)"
                    } else {
                        ""
                    };
                    write!(&mut log_msg, "Reactivated{suffix}   {signature}").unwrap();
                    // Register the tunnel endpoint as in use for this client
                    device_state.register_tunnel_endpoint(user.client_ip, tunnel_endpoint);
                    metrics::counter!(
                        "doublezero_activator_state_transition",
                        "state_transition" => "user-updating-to-activated",
                        "user-pubkey" => pubkey.to_string(),
                    )
                    .increment(1);
                    record_device_ip_metrics(&user.device_pk, device_state, locations, exchanges);
                }
                Err(e) => {
                    log_error_ignore_invalid_status(&mut log_msg, e);
                }
            }

            info!("{log_msg}");
        }

        // Delete User
        UserStatus::Deleting | UserStatus::PendingBan => {
            let mut log_msg = String::new();

            write!(
                &mut log_msg,
                "Event:User(Deleting) {} ({}) ",
                pubkey, user.client_ip
            )
            .unwrap();

            if let Some(device_state) = devices.get_mut(&user.device_pk) {
                write!(
                    &mut log_msg,
                    "for {} tunnel_net: {} tunnel_id: {} dz_ip: {}",
                    device_state.device.code, &user.tunnel_net, user.tunnel_id, &user.dz_ip
                )
                .unwrap();

                if user.status == UserStatus::Deleting {
                    let res = CloseAccountUserCommand {
                        pubkey: *pubkey,
                        use_onchain_deallocation: use_onchain_allocation,
                    }
                    .execute(client);

                    match res {
                        Ok(signature) => {
                            if use_onchain_allocation {
                                write!(&mut log_msg, " Deactivated (onchain) {signature}").unwrap();
                                // Onchain deallocation: smart contract handles releasing resources
                            } else {
                                write!(&mut log_msg, " Deactivated {signature}").unwrap();
                                // Off-chain: activator tracks local allocations
                                if user.has_unicast_tunnel() {
                                    link_ids.unassign(user.tunnel_id);
                                    user_tunnel_ips.unassign_block(user.tunnel_net.into());
                                }
                                deallocate_publisher_dz_ip(user, publisher_dz_ips);
                                if user.dz_ip != Ipv4Addr::UNSPECIFIED {
                                    device_state.release(user.dz_ip, user.tunnel_id).unwrap();
                                }
                            }
                            // Release the tunnel endpoint (activator-local state, always needed)
                            if user.has_tunnel_endpoint() {
                                device_state
                                    .release_tunnel_endpoint(user.client_ip, user.tunnel_endpoint);
                            }

                            metrics::counter!(
                                "doublezero_activator_state_transition",
                                "state_transition" => "user-deleting-to-deactivated",
                                "user-pubkey" => pubkey.to_string(),
                            )
                            .increment(1);
                        }
                        Err(e) => warn!("Error: {e}"),
                    }
                } else if user.status == UserStatus::PendingBan {
                    let res = BanUserCommand { pubkey: *pubkey }.execute(client);

                    match res {
                        Ok(signature) => {
                            write!(&mut log_msg, " Banned {signature}").unwrap();

                            if !use_onchain_allocation {
                                if user.has_unicast_tunnel() {
                                    link_ids.unassign(user.tunnel_id);
                                    user_tunnel_ips.unassign_block(user.tunnel_net.into());
                                }
                                deallocate_publisher_dz_ip(user, publisher_dz_ips);
                            }
                            if user.dz_ip != Ipv4Addr::UNSPECIFIED {
                                device_state.release(user.dz_ip, user.tunnel_id).unwrap();
                            }
                            // Release the tunnel endpoint
                            if user.has_tunnel_endpoint() {
                                device_state
                                    .release_tunnel_endpoint(user.client_ip, user.tunnel_endpoint);
                            }

                            metrics::counter!(
                                "doublezero_activator_state_transition",
                                "state_transition" => "user-pending-ban-to-banned",
                                "user-pubkey" => pubkey.to_string(),
                            )
                            .increment(1);
                        }
                        Err(e) => {
                            write!(&mut log_msg, "Error {e}").unwrap();
                        }
                    }
                }
                record_device_ip_metrics(&user.device_pk, device_state, locations, exchanges);
            }

            info!("{log_msg}");
        }
        _ => {}
    }
}

/// Resolve the tunnel endpoint for a user, rejecting if invalid or unavailable.
/// Returns `Some(endpoint)` on success, or `None` if the user was rejected.
fn resolve_tunnel_endpoint(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    device_state: &DeviceState,
    user: &User,
    log_msg: &mut String,
) -> Option<Ipv4Addr> {
    if user.has_tunnel_endpoint() {
        if device_state.is_valid_tunnel_endpoint(user.tunnel_endpoint) {
            Some(user.tunnel_endpoint)
        } else {
            log_reject(
                client,
                pubkey,
                "Error: Invalid tunnel endpoint requested",
                "Invalid tunnel endpoint",
                log_msg,
            );
            None
        }
    } else {
        match device_state.get_available_tunnel_endpoint(user.client_ip) {
            Some(ep) => Some(ep),
            None => {
                log_reject(
                    client,
                    pubkey,
                    "Error: No available tunnel endpoint",
                    "No available tunnel endpoint",
                    log_msg,
                );
                None
            }
        }
    }
}

fn deallocate_publisher_dz_ip(user: &User, publisher_dz_ips: &mut Option<IPBlockAllocator>) {
    if user.user_type == UserType::Multicast
        && !user.publishers.is_empty()
        && user.dz_ip != Ipv4Addr::UNSPECIFIED
        && user.dz_ip != user.client_ip
    {
        if let Some(ref mut publisher_ips) = publisher_dz_ips {
            if let Ok(dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
                publisher_ips.unassign_block(dz_ip_net.into());
                info!(
                    "Deallocated publisher dz_ip {} from global pool",
                    user.dz_ip
                );
            }
        }
    }
}

/// Reject a user and write the outcome to log_msg.
fn log_reject(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    reason: &str,
    label: &str,
    log_msg: &mut String,
) {
    let res = reject_user(client, pubkey, reason);
    match res {
        Ok(signature) => {
            write!(log_msg, " Reject({label}) Rejected {signature}").unwrap();
        }
        Err(e) => {
            write!(log_msg, " Reject({label}) Error: {e}").unwrap();
        }
    }
    info!("{log_msg}");
}

fn log_error_ignore_invalid_status(log_msg: &mut String, e: eyre::ErrReport) {
    // Ignore DoubleZeroError::InvalidStatus errors since this only happens when the user is already activated
    if let Some(dz_err) = e.downcast_ref::<DoubleZeroError>() {
        if matches!(dz_err, DoubleZeroError::InvalidStatus) {
            // Do nothing
        } else {
            write!(log_msg, "Error: {e}").unwrap();
        }
    } else {
        write!(log_msg, "Error: {e}").unwrap();
    }
}

fn reject_user(
    client: &dyn DoubleZeroClient,
    pubkey: &Pubkey,
    reason: &str,
) -> eyre::Result<Signature> {
    let signature = RejectUserCommand {
        pubkey: *pubkey,
        reason: reason.to_string(),
    }
    .execute(client)?;

    metrics::counter!(
        "doublezero_activator_state_transition",
        "state_transition" => "user-pending-to-rejected",
        "user-pubkey" => pubkey.to_string(),
    )
    .increment(1);

    Ok(signature)
}

fn get_or_insert_device_state<'a>(
    client: &dyn DoubleZeroClient,
    _pubkey: &Pubkey,
    devices: &'a mut DeviceMap,
    user: &User,
) -> Option<&'a mut DeviceState> {
    match devices.entry(user.device_pk) {
        Entry::Occupied(entry) => Some(entry.into_mut()),
        Entry::Vacant(entry) => {
            let res = GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            }
            .execute(client);

            match res {
                Ok((_, device)) => {
                    info!(
                        "Add Device: {} public_ip: {} dz_prefixes: {} ",
                        device.code, &device.public_ip, &device.dz_prefixes,
                    );
                    Some(entry.insert(DeviceState::new(&device)))
                }
                Err(_) => None,
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::{create_test_client, get_device_bump_seed, get_user_bump_seed};
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{
        AccountData, AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType,
        InterfaceStatus, InterfaceType, LoopbackType, MockDoubleZeroClient, UserCYOA, UserType,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::get_accesspass_pda,
        processors::user::{
            activate::UserActivateArgs, ban::UserBanArgs, closeaccount::UserCloseAccountArgs,
            reject::UserRejectArgs,
        },
        state::accesspass::{AccessPass, AccessPassStatus, AccessPassType},
    };
    use metrics_util::debugging::DebuggingRecorder;
    use mockall::{predicate, Sequence};

    fn do_test_process_user_event_pending_to_activated(
        user_type: UserType,
        expected_dz_ip: Option<Ipv4Addr>,
        expected_ips: u64,
    ) {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                reference_count: 0,
                bump_seed: get_device_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 2].into(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                code: "TestDevice".to_string(),
                dz_prefixes: "10.0.0.1/24".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            let (accesspass_pk_unspecified, _) = get_accesspass_pda(
                &client.get_program_id(),
                &Ipv4Addr::UNSPECIFIED,
                &user.owner,
            );
            let (accesspass_pk, _) =
                get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: user.owner,
                bump_seed: 255,
                accesspass_type: AccessPassType::Prepaid,
                client_ip: user.client_ip,
                user_payer: user.owner,
                last_access_epoch: 1234,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![Default::default()],
                flags: 0,
            };

            let user_clonned = user.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user_clonned.clone())));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk_unspecified))
                .returning(move |_| Err(eyre::eyre!("AccessPass not found")));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk))
                .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        tunnel_id: 500,
                        tunnel_net: "10.0.0.0/31".parse().unwrap(),
                        dz_ip: expected_dz_ip.unwrap_or(Ipv4Addr::UNSPECIFIED),
                        dz_prefix_count: 0, // legacy path
                        tunnel_endpoint: Ipv4Addr::new(192, 168, 1, 2),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None, // publisher_dz_ips
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            assert!(!user_tunnel_ips.assigned_ips.is_empty());
            assert!(!link_ids.assigned.is_empty());

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    expected_ips,
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    256,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-activated"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl() {
        do_test_process_user_event_pending_to_activated(
            UserType::IBRL,
            Some([192, 168, 1, 1].into()),
            0,
        );
    }

    #[test]
    fn test_process_user_event_pending_to_activated_ibrl_with_allocated_ip() {
        do_test_process_user_event_pending_to_activated(
            UserType::IBRLWithAllocatedIP,
            Some([10, 0, 0, 1].into()),
            1,
        );
    }

    #[test]
    fn test_process_user_event_pending_to_activated_edge_filtering() {
        do_test_process_user_event_pending_to_activated(
            UserType::EdgeFiltering,
            Some([10, 0, 0, 1].into()),
            1,
        );
    }

    #[test]
    fn test_process_user_event_update_to_activated() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                reference_count: 0,
                bump_seed: get_device_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 2].into(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                code: "TestDevice".to_string(),
                dz_prefixes: "10.0.0.1/24".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::Multicast,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: [192, 168, 1, 1].into(),
                tunnel_id: 500,
                tunnel_net: "10.0.0.1/29".parse().unwrap(),
                status: UserStatus::Updating,
                publishers: vec![Pubkey::default()],
                subscribers: vec![Pubkey::default()],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            let (accesspass_pk_unspecified, _) = get_accesspass_pda(
                &client.get_program_id(),
                &Ipv4Addr::UNSPECIFIED,
                &user.owner,
            );
            let (accesspass_pk, _) =
                get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: user.owner,
                bump_seed: 255,
                accesspass_type: AccessPassType::Prepaid,
                client_ip: user.client_ip,
                user_payer: user.owner,
                last_access_epoch: 1234,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![Default::default()],
                flags: 0,
            };

            // ActivateUserCommand internally fetches user and access pass
            let user_cloned = user.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user_cloned.clone())));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk_unspecified))
                .returning(move |_| Err(eyre::eyre!("AccessPass not found")));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk))
                .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        tunnel_id: 500,
                        tunnel_net: "10.0.0.1/29".parse().unwrap(),
                        dz_ip: [147, 51, 126, 1].into(),
                        dz_prefix_count: 0, // legacy path
                        tunnel_endpoint: Ipv4Addr::new(192, 168, 1, 2),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            let mut publisher_dz_ips =
                Some(IPBlockAllocator::new("147.51.126.0/23".parse().unwrap()));

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut publisher_dz_ips,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            assert!(!user_tunnel_ips.assigned_ips.is_empty());
            assert!(!link_ids.assigned.is_empty());

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    0, // publisher IP comes from global pool, not device
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    256,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-updating-to-activated"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_get_device() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRLWithAllocatedIP,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(device_pubkey))
                .returning(|_| Err(eyre::eyre!("Device not found")));

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                        reason: "Error: Device not found".to_string(),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None, // publisher_dz_ips
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-rejected"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_tunnel_block() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                reference_count: 0,
                bump_seed: get_device_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 2].into(),
                status: DeviceStatus::Activated,
                code: "TestDevice".to_string(),
                metrics_publisher_pk: Pubkey::default(),
                dz_prefixes: "10.0.0.0/32".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRLWithAllocatedIP,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                        reason: "Error: No available dz_ip to allocate".to_string(),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            let device2 = device.clone();
            devices.insert(device_pubkey, DeviceState::new(&device2));

            // allocate the only ip
            assert_ne!(
                devices.get_mut(&device_pubkey).unwrap().dz_ips[0].next_available_block(1, 1),
                None
            );

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None, // publisher_dz_ips
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-rejected"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_to_rejected_by_no_user_block() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/32".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            // eat a blocok
            let _ = user_tunnel_ips.next_available_block(0, 2);

            let device_pubkey = Pubkey::new_unique();
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                reference_count: 0,
                bump_seed: get_device_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 2].into(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                code: "TestDevice".to_string(),
                dz_prefixes: "10.0.0.1/24".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRLWithAllocatedIP,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                        reason: "Error: No available user block".to_string(),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            let device2 = device.clone();
            devices.insert(device_pubkey, DeviceState::new(&device2));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None, // publisher_dz_ips
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-rejected"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    fn do_test_process_user_event_deleting_or_pending_ban<F>(
        user_status: UserStatus,
        func: F,
        state_transition: &'static str,
    ) where
        F: Fn(&mut MockDoubleZeroClient, &User, &mut Sequence),
    {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            assert!(user_status == UserStatus::Deleting || user_status == UserStatus::PendingBan);

            let mut seq = Sequence::new();
            let mut devices = HashMap::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRLWithAllocatedIP,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 102,
                tunnel_net: "10.0.0.0/31".parse().unwrap(),
                status: user_status,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            };

            let user2 = user.clone();
            client
                .expect_get()
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user2.clone())));

            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: 0,
                reference_count: 0,
                bump_seed: get_device_bump_seed(&client),
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type: DeviceType::Hybrid,
                public_ip: [192, 168, 1, 2].into(),
                status: DeviceStatus::Activated,
                code: "TestDevice".to_string(),
                metrics_publisher_pk: Pubkey::default(),
                dz_prefixes: "11.0.0.0/16".parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };

            devices.insert(device_pubkey, DeviceState::new(&device));

            func(&mut client, &user, &mut seq);

            assert!(link_ids.assigned.contains(&102));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None, // publisher_dz_ips
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false, // use_onchain_allocation
            );

            assert!(!link_ids.assigned.contains(&102));

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    0,
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    65536,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", state_transition),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_deleting() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::Deleting,
            |user_service, _, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::CloseAccountUser(
                            UserCloseAccountArgs {
                                dz_prefix_count: 0, // legacy path
                                multicast_publisher_count: 0,
                            },
                        )),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
            "user-deleting-to-deactivated",
        );
    }

    #[test]
    fn test_process_user_event_pending_ban() {
        do_test_process_user_event_deleting_or_pending_ban(
            UserStatus::PendingBan,
            |user_service, _, seq| {
                user_service
                    .expect_execute_transaction()
                    .times(1)
                    .in_sequence(seq)
                    .with(
                        predicate::eq(DoubleZeroInstruction::BanUser(UserBanArgs {})),
                        predicate::always(),
                    )
                    .returning(|_, _| Ok(Signature::new_unique()));
            },
            "user-pending-ban-to-banned",
        );
    }

    /// Helper to create a device with UserTunnelEndpoint interfaces.
    fn create_device_with_tunnel_endpoints(bump_seed: u8, endpoints: Vec<Ipv4Addr>) -> Device {
        let interfaces = endpoints
            .into_iter()
            .enumerate()
            .map(|(i, ip)| {
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: format!("Loopback{}", 100 + i),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: format!("{}/32", ip).parse().unwrap(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: true,
                    ..Default::default()
                }
                .to_interface()
            })
            .collect();

        Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            reference_count: 0,
            bump_seed,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 2].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "TestDevice".to_string(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces,
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        }
    }

    #[test]
    fn test_process_user_event_pending_with_valid_demanded_tunnel_endpoint() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let demanded_endpoint = Ipv4Addr::new(5, 5, 5, 5);
            let device = create_device_with_tunnel_endpoints(
                get_device_bump_seed(&client),
                vec![demanded_endpoint, Ipv4Addr::new(6, 6, 6, 6)],
            );

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRL,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: demanded_endpoint,
            };

            let (accesspass_pk_unspecified, _) = get_accesspass_pda(
                &client.get_program_id(),
                &Ipv4Addr::UNSPECIFIED,
                &user.owner,
            );
            let (accesspass_pk, _) =
                get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: user.owner,
                bump_seed: 255,
                accesspass_type: AccessPassType::Prepaid,
                client_ip: user.client_ip,
                user_payer: user.owner,
                last_access_epoch: 1234,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![Default::default()],
                flags: 0,
            };

            let user_cloned = user.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user_cloned.clone())));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk_unspecified))
                .returning(move |_| Err(eyre::eyre!("AccessPass not found")));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk))
                .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

            // The activator should use the demanded endpoint (5.5.5.5), not pick one itself
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        tunnel_id: 500,
                        tunnel_net: "10.0.0.0/31".parse().unwrap(),
                        dz_ip: Ipv4Addr::new(192, 168, 1, 1),
                        dz_prefix_count: 0,
                        tunnel_endpoint: demanded_endpoint,
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false,
            );

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    0,
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    256,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-activated"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_with_invalid_demanded_tunnel_endpoint() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            // Device only has 5.5.5.5 as a valid endpoint
            let device = create_device_with_tunnel_endpoints(
                get_device_bump_seed(&client),
                vec![Ipv4Addr::new(5, 5, 5, 5)],
            );

            let user_pubkey = Pubkey::new_unique();
            // User demands 9.9.9.9 which is NOT a valid endpoint on this device
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRL,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::new(9, 9, 9, 9),
            };

            // Expect a reject transaction, NOT an activate
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                        reason: "Error: Invalid tunnel endpoint requested".to_string(),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false,
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-rejected"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_pending_with_unspecified_tunnel_endpoint_falls_back() {
        // When tunnel_endpoint is 0.0.0.0, activator should pick one via
        // get_available_tunnel_endpoint (backwards compat). This is the same as the
        // existing do_test_process_user_event_pending_to_activated tests, but with
        // explicit UTE interfaces on the device to verify the first-available logic.
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let device = create_device_with_tunnel_endpoints(
                get_device_bump_seed(&client),
                vec![Ipv4Addr::new(5, 5, 5, 5), Ipv4Addr::new(6, 6, 6, 6)],
            );

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::IBRL,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: Ipv4Addr::UNSPECIFIED,
                tunnel_id: 0,
                tunnel_net: NetworkV4::default(),
                status: UserStatus::Pending,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED, // no demand
            };

            let (accesspass_pk_unspecified, _) = get_accesspass_pda(
                &client.get_program_id(),
                &Ipv4Addr::UNSPECIFIED,
                &user.owner,
            );
            let (accesspass_pk, _) =
                get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: user.owner,
                bump_seed: 255,
                accesspass_type: AccessPassType::Prepaid,
                client_ip: user.client_ip,
                user_payer: user.owner,
                last_access_epoch: 1234,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![Default::default()],
                flags: 0,
            };

            let user_cloned = user.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user_cloned.clone())));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk_unspecified))
                .returning(move |_| Err(eyre::eyre!("AccessPass not found")));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk))
                .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

            // With 0.0.0.0, the activator should fall back to first-available = 5.5.5.5
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        tunnel_id: 500,
                        tunnel_net: "10.0.0.0/31".parse().unwrap(),
                        dz_ip: Ipv4Addr::new(192, 168, 1, 1),
                        dz_prefix_count: 0,
                        tunnel_endpoint: Ipv4Addr::new(5, 5, 5, 5),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false,
            );

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    0,
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    256,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-activated"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_updating_with_valid_demanded_tunnel_endpoint() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let demanded_endpoint = Ipv4Addr::new(6, 6, 6, 6);
            let device = create_device_with_tunnel_endpoints(
                get_device_bump_seed(&client),
                vec![Ipv4Addr::new(5, 5, 5, 5), demanded_endpoint],
            );

            let user_pubkey = Pubkey::new_unique();
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::Multicast,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: [192, 168, 1, 1].into(),
                tunnel_id: 500,
                tunnel_net: "10.0.0.1/29".parse().unwrap(),
                status: UserStatus::Updating,
                publishers: vec![Pubkey::default()],
                subscribers: vec![Pubkey::default()],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: demanded_endpoint,
            };

            let (accesspass_pk_unspecified, _) = get_accesspass_pda(
                &client.get_program_id(),
                &Ipv4Addr::UNSPECIFIED,
                &user.owner,
            );
            let (accesspass_pk, _) =
                get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: user.owner,
                bump_seed: 255,
                accesspass_type: AccessPassType::Prepaid,
                client_ip: user.client_ip,
                user_payer: user.owner,
                last_access_epoch: 1234,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![Default::default()],
                flags: 0,
            };

            // ActivateUserCommand internally fetches user and access pass
            let user_cloned = user.clone();
            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(user_pubkey))
                .returning(move |_| Ok(AccountData::User(user_cloned.clone())));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk_unspecified))
                .returning(move |_| Err(eyre::eyre!("AccessPass not found")));

            client
                .expect_get()
                .times(1)
                .in_sequence(&mut seq)
                .with(predicate::eq(accesspass_pk))
                .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

            // Should use the demanded endpoint 6.6.6.6
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        tunnel_id: 500,
                        tunnel_net: "10.0.0.1/29".parse().unwrap(),
                        dz_ip: [147, 51, 126, 1].into(),
                        dz_prefix_count: 0,
                        tunnel_endpoint: demanded_endpoint,
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            let mut publisher_dz_ips =
                Some(IPBlockAllocator::new("147.51.126.0/23".parse().unwrap()));

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut publisher_dz_ips,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false,
            );

            let device_pk_str = user.device_pk.to_string();

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_device_assigned_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    0, // publisher IP comes from global pool, not device
                )
                .expect_counter(
                    "doublezero_activator_device_total_ips",
                    vec![
                        ("device_pk", device_pk_str.as_str()),
                        ("code", "TestDevice"),
                    ],
                    256,
                )
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-updating-to-activated"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }

    #[test]
    fn test_process_user_event_updating_with_invalid_demanded_tunnel_endpoint() {
        let recorder = DebuggingRecorder::new();
        let snapshotter = recorder.snapshotter();

        metrics::with_local_recorder(&recorder, || {
            let mut seq = Sequence::new();
            let mut user_tunnel_ips = IPBlockAllocator::new("10.0.0.0/16".parse().unwrap());
            let mut link_ids = IDAllocator::new(100, vec![100, 101, 102]);
            let mut client = create_test_client();

            let device_pubkey = Pubkey::new_unique();
            let device = create_device_with_tunnel_endpoints(
                get_device_bump_seed(&client),
                vec![Ipv4Addr::new(5, 5, 5, 5)],
            );

            let user_pubkey = Pubkey::new_unique();
            // User demands 9.9.9.9 which is NOT a valid endpoint on this device
            let user = User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 0,
                bump_seed: get_user_bump_seed(&client),
                user_type: UserType::Multicast,
                tenant_pk: Pubkey::new_unique(),
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [192, 168, 1, 1].into(),
                dz_ip: [192, 168, 1, 1].into(),
                tunnel_id: 500,
                tunnel_net: "10.0.0.1/29".parse().unwrap(),
                status: UserStatus::Updating,
                publishers: vec![Pubkey::default()],
                subscribers: vec![Pubkey::default()],
                validator_pubkey: Pubkey::default(),
                tunnel_endpoint: Ipv4Addr::new(9, 9, 9, 9),
            };

            // Expect a reject transaction
            client
                .expect_execute_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(DoubleZeroInstruction::RejectUser(UserRejectArgs {
                        reason: "Error: Invalid tunnel endpoint requested".to_string(),
                    })),
                    predicate::always(),
                )
                .returning(|_, _| Ok(Signature::new_unique()));

            let mut devices = HashMap::new();
            devices.insert(device_pubkey, DeviceState::new(&device));

            let locations = HashMap::<Pubkey, Location>::new();
            let exchanges = HashMap::<Pubkey, Exchange>::new();

            process_user_event(
                &client,
                &user_pubkey,
                &mut devices,
                &mut user_tunnel_ips,
                &mut None,
                &mut link_ids,
                &user,
                &locations,
                &exchanges,
                false,
            );

            let mut snapshot = crate::test_helpers::MetricsSnapshot::new(snapshotter.snapshot());
            snapshot
                .expect_counter(
                    "doublezero_activator_state_transition",
                    vec![
                        ("state_transition", "user-pending-to-rejected"),
                        ("user-pubkey", user_pubkey.to_string().as_str()),
                    ],
                    1,
                )
                .verify();
        });
    }
}
