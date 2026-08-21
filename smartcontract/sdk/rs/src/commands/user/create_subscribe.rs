use doublezero_ip_proof::IpOwnershipProof;
use doublezero_serviceability::{
    pda::get_user_pda,
    processors::user::create_subscribe::UserCreateSubscribeArgs,
    state::{
        multicastgroup::MulticastGroupStatus,
        user::{UserCYOA, UserType},
    },
};
use doublezero_serviceability_instruction::{compute_budget_prelude, user::create_subscribe_user};
use eyre::Context;
use solana_sdk::{message::Message, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        multicastgroup::get::GetMulticastGroupCommand, user::instructions_with_ip_proof,
    },
    DoubleZeroClient,
};

/// Solana's transaction wire-size limit (`solana_packet::PACKET_DATA_SIZE`, not
/// re-exported by the SDK crates this one depends on).
const PACKET_DATA_SIZE: usize = 1232;

/// Wire-size room reserved for the read-only Permission PDA `build_with_permission`
/// appends at the permission rollout: one more account key plus its index byte.
const PERMISSION_ACCOUNT_RESERVED: usize = 33;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateSubscribeUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    /// Multicast groups the user is subscribed to at creation, atomically in one
    /// transaction. Must be non-empty; the first entry is the instruction's primary
    /// group and the rest ride as extra group accounts. The publisher/subscriber
    /// flags apply to every group.
    pub mgroup_pks: Vec<Pubkey>,
    pub publisher: bool,
    pub subscriber: bool,
    pub tunnel_endpoint: Ipv4Addr,
    /// Custom owner pubkey (foundation allowlist only). When set, the access pass
    /// is looked up for this owner instead of the payer.
    pub owner: Option<Pubkey>,
    /// Optional trailing Feed account for the EdgeSeat metro gate: the feed (referenced
    /// by the pass) covering the device's exchange and listing the target multicast group.
    /// Appended to the account list only when provided.
    pub feed_pk: Option<Pubkey>,
    /// RFC-27 proof that the user's owner originated a request from `client_ip`, obtained from the
    /// DoubleZero IP verification service. Supplying it also pulls the Instructions sysvar into
    /// the account list and a native `Ed25519SigVerify` instruction into the transaction; leaving
    /// it `None` produces the pre-RFC-27 shape, which the program accepts until
    /// `require-ip-ownership-proof` is set for the environment.
    ///
    /// On the `owner` override path the proof must name that owner, not the payer: the program
    /// binds it to the user's effective owner.
    pub ip_proof: Option<IpOwnershipProof>,
}

impl CreateSubscribeUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        // Deduplicate while preserving order: the processor rejects duplicate group
        // accounts in a batch.
        let mut mgroup_pks: Vec<Pubkey> = Vec::with_capacity(self.mgroup_pks.len());
        for pk in &self.mgroup_pks {
            if !mgroup_pks.contains(pk) {
                mgroup_pks.push(*pk);
            }
        }
        // `extra_group_count` is a u8 on the wire, so more extras cannot even be
        // encoded; bail before the per-group validation round-trips. The real bound
        // is the transaction size, checked below once the account list is known.
        if mgroup_pks.len() > usize::from(u8::MAX) + 1 {
            eyre::bail!(
                "{} multicast groups can never fit one transaction; subscribe the rest via UpdateMulticastGroupRolesCommand",
                mgroup_pks.len()
            );
        }
        let (first_mgroup_pk, extra_mgroup_pks) = mgroup_pks
            .split_first()
            .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

        for mgroup_pk in &mgroup_pks {
            let (_, mgroup) = GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pk.to_string(),
            }
            .execute(client)
            .wrap_err_with(|| format!("MulticastGroup not found ({mgroup_pk})"))?;

            if mgroup.status != MulticastGroupStatus::Activated {
                eyre::bail!("MulticastGroup not active ({mgroup_pk})");
            }
        }

        // When a custom owner is set, look up the access pass for that owner
        let accesspass_payer = self.owner.unwrap_or_else(|| client.get_payer());

        // GetAccessPassCommand prefers a shared dynamic (UNSPECIFIED) pass and falls
        // back to the exact client-IP pass, matching the onchain create_user path.
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: accesspass_payer,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("No Access Pass found for owner"))?;

        let program_id = client.get_program_id();
        let (pda_pubkey, _) = get_user_pda(&program_id, &self.client_ip, self.user_type);

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.device_pk.to_string(),
        }
        .execute(client)
        .wrap_err_with(|| format!("Device not found ({})", self.device_pk))?;
        let dz_prefix_count = device.dz_prefixes.len();
        if dz_prefix_count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot create user",
                self.device_pk
            ));
        }
        let dz_prefix_count_u8 = u8::try_from(dz_prefix_count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                self.device_pk,
                dz_prefix_count
            )
        })?;

        let ix = create_subscribe_user(
            &program_id,
            &client.get_payer(),
            &self.device_pk,
            first_mgroup_pk,
            &accesspass_pk,
            dz_prefix_count_u8,
            extra_mgroup_pks,
            self.feed_pk.as_ref(),
            UserCreateSubscribeArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                client_ip: self.client_ip,
                publisher: self.publisher,
                subscriber: self.subscriber,
                tunnel_endpoint: self.tunnel_endpoint,
                dz_prefix_count: dz_prefix_count_u8,
                owner: self.owner.unwrap_or_default(),
                ip_proof: self.ip_proof,
                extra_group_count: 0, // derived by the builder from extra_mgroup_pks
            },
        );

        // Pair the creation with the Ed25519 instruction that proves its proof before measuring:
        // RFC-27 adds that instruction and two account keys to the same transaction, so the group
        // cap below has to be judged against what actually goes on the wire.
        let mut instructions = match &self.ip_proof {
            // `accesspass_payer` is the user's effective owner — the same key the program binds
            // the proof to.
            Some(proof) => instructions_with_ip_proof(
                client,
                proof,
                &accesspass_payer,
                &self.client_ip,
                self.user_type as u8,
                ix,
            )?,
            None => vec![ix],
        };

        // Unlike a role update, this transaction also carries the device, one account
        // per device dz_prefix, and an optional feed, so no fixed group cap can bound
        // it (16 groups fit a role update but overflow a create on a five-prefix
        // device with a feed). Measure the wire size of the exact transaction
        // send_transaction builds, reserving room for the Permission PDA the builder
        // appends at the permission rollout.
        let [cu_limit, heap_frame] = compute_budget_prelude();
        let mut all_instructions = vec![cu_limit, heap_frame];
        all_instructions.extend(instructions.iter().cloned());
        let message = Message::new(&all_instructions, Some(&client.get_payer()));
        let tx_size = 1
            + 64 * usize::from(message.header.num_required_signatures)
            + message.serialize().len();
        if tx_size + PERMISSION_ACCOUNT_RESERVED > PACKET_DATA_SIZE {
            // Every extra group past the fit costs its 32-byte key plus an index byte.
            let over = tx_size + PERMISSION_ACCOUNT_RESERVED - PACKET_DATA_SIZE;
            eyre::bail!(
                "subscribing {} multicast groups at create builds a {tx_size}-byte transaction, \
                 over the {PACKET_DATA_SIZE}-byte limit ({dz_prefix_count} dz_prefix account(s){} \
                 ride along); at most {} group(s) fit; subscribe the rest via \
                 UpdateMulticastGroupRolesCommand after activation",
                mgroup_pks.len(),
                if self.feed_pk.is_some() {
                    " and a feed"
                } else {
                    ""
                },
                mgroup_pks.len().saturating_sub(over.div_ceil(33)),
            );
        }

        let signature = match instructions.len() {
            // No proof: the pre-RFC-27 single-instruction send, byte for byte.
            1 => client.send_transaction(instructions.pop().expect("length checked"))?,
            _ => client.send_instructions(instructions)?,
        };

        Ok((signature, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::create_subscribe::CreateSubscribeUserCommand,
        tests::utils::{create_test_client, create_test_client_with_ip_verifier},
        DoubleZeroClient,
    };
    use doublezero_ip_proof::IpOwnershipProof;
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        processors::user::create_subscribe::UserCreateSubscribeArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{UserCYOA, UserType},
        },
    };
    use doublezero_serviceability_instruction::{
        ip_proof::with_ed25519_verification, user::create_subscribe_user,
    };
    use mockall::predicate;
    use solana_sdk::{
        pubkey::Pubkey,
        signature::{Keypair, Signature},
        signer::Signer,
    };
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_create_subscribe() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let mgroup_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let mgroup = MulticastGroup {
            status: MulticastGroupStatus::Activated,
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(mgroup_pk))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &payer);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // GetAccessPassCommand checks the UNSPECIFIED (dynamic) PDA first; no pass
        // exists there, so it falls back to the exact-IP PDA above.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        let expected = create_subscribe_user(
            &program_id,
            &payer,
            &device_pk,
            &mgroup_pk,
            &accesspass_pubkey,
            1,
            &[],
            None,
            UserCreateSubscribeArgs {
                user_type: UserType::IBRLWithAllocatedIP,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip,
                publisher: true,
                subscriber: false,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                owner: Pubkey::default(),
                ip_proof: None,
                extra_group_count: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateSubscribeUserCommand {
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            mgroup_pks: vec![mgroup_pk],
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            owner: None,
            feed_pk: None,
            ip_proof: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    /// Mock the lookups a create with `mgroup_pks` needs: every group Activated, the
    /// access pass at the exact-IP PDA, and a device advertising `dz_prefixes`.
    ///
    /// `accesspass_owner` is the key the pass belongs to — the payer, or the `--owner`
    /// override on the foundation-allowlist path.
    fn expect_create_lookups(
        client: &mut crate::MockDoubleZeroClient,
        accesspass_owner: Pubkey,
        mgroup_pks: &[Pubkey],
        device_pk: Pubkey,
        client_ip: Ipv4Addr,
        dz_prefixes: &str,
    ) {
        let program_id = client.get_program_id();

        for mgroup_pk in mgroup_pks.iter().copied() {
            let mgroup = MulticastGroup {
                status: MulticastGroupStatus::Activated,
                ..Default::default()
            };
            client
                .expect_get()
                .with(predicate::eq(mgroup_pk))
                .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));
        }

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &accesspass_owner);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: accesspass_owner,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: accesspass_owner,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &accesspass_owner);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: dz_prefixes.parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
    }

    const FIVE_PREFIXES: &str = "10.0.0.0/24,10.0.1.0/24,10.0.2.0/24,10.0.3.0/24,10.0.4.0/24";

    /// A batch a role update would accept (16 groups) overflows the create
    /// transaction on a five-prefix device with a feed — blocked in the SDK with the
    /// measured size, before anything is sent.
    #[test]
    fn test_commands_user_create_subscribe_rejects_a_batch_over_the_size_limit() {
        let mut client = create_test_client();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        let mgroup_pks: Vec<Pubkey> = (0..16).map(|_| Pubkey::new_unique()).collect();
        let payer = client.get_payer();
        expect_create_lookups(
            &mut client,
            payer,
            &mgroup_pks,
            device_pk,
            client_ip,
            FIVE_PREFIXES,
        );
        client.expect_send_transaction().times(0);

        let err = CreateSubscribeUserCommand {
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            mgroup_pks,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            owner: None,
            feed_pk: Some(Pubkey::new_unique()),
            ip_proof: None,
        }
        .execute(&client)
        .unwrap_err();
        assert!(
            err.to_string().contains("1232-byte limit"),
            "unexpected error: {err}"
        );
    }

    /// The daemon folds at most eight groups into a create (`MAX_CREATE_GROUPS`);
    /// that batch fits even on a five-prefix device with a feed.
    #[test]
    fn test_commands_user_create_subscribe_max_daemon_fold_fits_the_size_limit() {
        let mut client = create_test_client();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        let mgroup_pks: Vec<Pubkey> = (0..8).map(|_| Pubkey::new_unique()).collect();
        let payer = client.get_payer();
        expect_create_lookups(
            &mut client,
            payer,
            &mgroup_pks,
            device_pk,
            client_ip,
            FIVE_PREFIXES,
        );
        client
            .expect_send_transaction()
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateSubscribeUserCommand {
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            mgroup_pks,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            owner: None,
            feed_pk: Some(Pubkey::new_unique()),
            ip_proof: None,
        }
        .execute(&client);
        assert!(res.is_ok(), "{res:?}");
    }

    /// The RFC-27 path on the `--owner` override: the access pass, and therefore the proof, belong
    /// to the owner, not the payer. The program binds the proof to the user's effective owner, so
    /// a proof naming the payer here would be rejected onchain.
    #[test]
    fn test_commands_user_create_subscribe_with_ip_proof_for_the_owner() {
        let verifier = Keypair::new();
        let mut client = create_test_client_with_ip_verifier(verifier.pubkey());

        let program_id = client.get_program_id();
        let device_pk = Pubkey::new_unique();
        let mgroup_pk = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        expect_create_lookups(
            &mut client,
            owner,
            &[mgroup_pk],
            device_pk,
            client_ip,
            "10.0.0.0/24",
        );

        let proof = proof_for(&verifier, owner, client_ip);
        let expected_create = create_subscribe_user(
            &program_id,
            &client.get_payer(),
            &device_pk,
            &mgroup_pk,
            &get_accesspass_pda(&program_id, &client_ip, &owner).0,
            1,
            &[],
            None,
            UserCreateSubscribeArgs {
                user_type: UserType::IBRLWithAllocatedIP,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip,
                publisher: true,
                subscriber: false,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                owner,
                ip_proof: Some(proof),
                extra_group_count: 0,
            },
        );
        let expected =
            with_ed25519_verification(&verifier.pubkey(), &proof, expected_create).to_vec();
        client
            .expect_send_instructions()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res =
            command(client_ip, device_pk, mgroup_pk, Some(owner), Some(proof)).execute(&client);
        assert!(res.is_ok(), "{res:?}");
    }

    /// On the override path a proof naming the payer instead of the owner is exactly the mistake
    /// the local check exists to catch.
    #[test]
    fn test_commands_user_create_subscribe_with_ip_proof_rejects_payer_bound_proof() {
        let verifier = Keypair::new();
        let mut client = create_test_client_with_ip_verifier(verifier.pubkey());

        let payer = client.get_payer();
        let device_pk = Pubkey::new_unique();
        let mgroup_pk = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        expect_create_lookups(
            &mut client,
            owner,
            &[mgroup_pk],
            device_pk,
            client_ip,
            "10.0.0.0/24",
        );

        let err = command(
            client_ip,
            device_pk,
            mgroup_pk,
            Some(owner),
            Some(proof_for(&verifier, payer, client_ip)),
        )
        .execute(&client)
        .expect_err("a proof bound to the payer must not be sent for an owner-override user");
        assert!(err.to_string().contains("was issued for"), "{err}");
    }

    fn command(
        client_ip: Ipv4Addr,
        device_pk: Pubkey,
        mgroup_pk: Pubkey,
        owner: Option<Pubkey>,
        ip_proof: Option<IpOwnershipProof>,
    ) -> CreateSubscribeUserCommand {
        CreateSubscribeUserCommand {
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            mgroup_pks: vec![mgroup_pk],
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            owner,
            feed_pk: None,
            ip_proof,
        }
    }

    /// A proof the command will accept: signed by `verifier`, naming the user's effective owner.
    fn proof_for(verifier: &Keypair, owner: Pubkey, client_ip: Ipv4Addr) -> IpOwnershipProof {
        doublezero_ip_proof::sign(
            verifier,
            &owner,
            &client_ip,
            931,
            UserType::IBRLWithAllocatedIP as u8,
        )
    }
}
