//! RFC-27 IP ownership proof enforcement in user creation (issue #4197).
//!
//! The proof is what stops a caller binding a `client_ip` they cannot originate traffic from. The
//! signature itself is checked by the native Ed25519 precompile; the program's job — and what
//! these tests exercise — is confirming that the precompile instruction in the transaction covers
//! the message this creation implies, with the verifier key global state names.
//!
//! The negative cases split into two families, and both matter:
//!
//! - the *proof* disagrees with the request (wrong payer, IP, user account, epoch), which the
//!   program catches by reconstruction; and
//! - the *Ed25519 instruction* disagrees with the proof, or is shaped so that what the precompile
//!   verified is not what the program reads. That second family is where a naive implementation
//!   is exploitable, so the offsets get their own tests.

use doublezero_ip_proof::{
    sign, sign_version, signed_message_for, IpOwnershipProof, IP_PROOF_VERSION,
};
use doublezero_serviceability::{
    entrypoint::process_instruction,
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_exchange_pda,
        get_globalconfig_pda, get_globalstate_pda, get_location_pda, get_multicastgroup_pda,
        get_resource_extension_pda, get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        exchange::create::ExchangeCreateArgs,
        globalstate::{setauthority::SetAuthorityArgs, setfeatureflags::SetFeatureFlagsArgs},
        location::create::LocationCreateArgs,
        multicastgroup::{
            allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        user::{create::UserCreateArgs, create_subscribe::UserCreateSubscribeArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        accountdata::AccountData,
        device::DeviceType,
        feature_flags::FeatureFlag,
        user::{UserCYOA, UserType},
    },
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_keypair::Keypair;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

/// The address every test binds unless it is deliberately testing a mismatch.
const CLIENT_IP: Ipv4Addr = Ipv4Addr::new(100, 0, 0, 1);
/// A second globally-routable address, for the "proof issued for a different IP" case.
const OTHER_IP: Ipv4Addr = Ipv4Addr::new(100, 0, 0, 2);

/// Everything a test needs to submit a user creation and reason about the proof it carries.
struct Fixture {
    /// The whole context rather than just a `BanksClient`, so the epoch-window tests can warp the
    /// clock forward; every other test only ever touches `context.banks_client`.
    context: ProgramTestContext,
    payer: Keypair,
    program_id: Pubkey,
    globalstate: Pubkey,
    device: Pubkey,
    accesspass: Pubkey,
    mgroup: Pubkey,
    /// The verifier keypair whose public key the fixture wrote into global state.
    verifier: Keypair,
    resources: Resources,
}

#[derive(Clone, Copy)]
struct Resources {
    user_tunnel_block: Pubkey,
    multicast_publisher_block: Pubkey,
    tunnel_ids: Pubkey,
    dz_prefix_block: Pubkey,
}

impl Fixture {
    fn banks(&mut self) -> &mut BanksClient {
        &mut self.context.banks_client
    }

    /// The epoch `Clock::get()` reports inside the program right now.
    async fn current_epoch(&mut self) -> u64 {
        self.context
            .banks_client
            .get_sysvar::<solana_sdk::clock::Clock>()
            .await
            .expect("clock sysvar")
            .epoch
    }

    fn user_pda(&self, client_ip: Ipv4Addr, user_type: UserType) -> Pubkey {
        get_user_pda(&self.program_id, &client_ip, user_type).0
    }

    /// A proof the fixture's verifier actually signed. Every field is a parameter so a test can
    /// bend exactly one of them and leave the signature genuine — which is the point: a proof with
    /// a valid signature over the *wrong* message must still be rejected, and that is a different
    /// failure from a forged signature.
    fn proof(
        &self,
        payer: &Pubkey,
        client_ip: Ipv4Addr,
        epoch: u64,
        user_type: UserType,
    ) -> IpOwnershipProof {
        sign(&self.verifier, payer, &client_ip, epoch, user_type as u8)
    }

    /// The proof a well-formed `CreateUser` for `CLIENT_IP` carries at epoch 0.
    fn valid_proof(&self) -> IpOwnershipProof {
        self.proof(&self.payer.pubkey(), CLIENT_IP, 0, UserType::IBRL)
    }

    fn create_user_accounts(&self, user: Pubkey) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(user, false),
            AccountMeta::new(self.device, false),
            AccountMeta::new(self.accesspass, false),
            AccountMeta::new(self.globalstate, false),
            AccountMeta::new(self.resources.user_tunnel_block, false),
            AccountMeta::new(self.resources.multicast_publisher_block, false),
            AccountMeta::new(self.resources.tunnel_ids, false),
            AccountMeta::new(self.resources.dz_prefix_block, false),
        ]
    }

    fn create_subscribe_accounts(&self, user: Pubkey) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(user, false),
            AccountMeta::new(self.device, false),
            AccountMeta::new(self.mgroup, false),
            AccountMeta::new(self.accesspass, false),
            AccountMeta::new(self.globalstate, false),
            AccountMeta::new(self.resources.user_tunnel_block, false),
            AccountMeta::new(self.resources.multicast_publisher_block, false),
            AccountMeta::new(self.resources.tunnel_ids, false),
            AccountMeta::new(self.resources.dz_prefix_block, false),
        ]
    }

    fn create_user_args(
        &self,
        client_ip: Ipv4Addr,
        proof: Option<IpOwnershipProof>,
    ) -> UserCreateArgs {
        UserCreateArgs {
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: proof,
        }
    }

    /// Submits `CreateUser` for `CLIENT_IP` with the given proof, optionally preceded by an
    /// Ed25519 instruction, with the Instructions sysvar appended when `with_sysvar`.
    async fn create_user(
        &mut self,
        proof: Option<IpOwnershipProof>,
        prelude: &[solana_sdk::instruction::Instruction],
        with_sysvar: bool,
    ) -> Result<(), BanksClientError> {
        let user = self.user_pda(CLIENT_IP, UserType::IBRL);
        let mut accounts = self.create_user_accounts(user);
        accounts.push(AccountMeta::new(self.payer.pubkey(), true));
        accounts.push(AccountMeta::new(
            solana_system_interface::program::ID,
            false,
        ));
        if with_sysvar {
            accounts.push(instructions_sysvar_meta());
        }

        let mut instructions = prelude.to_vec();
        instructions.push(solana_sdk::instruction::Instruction::new_with_bytes(
            self.program_id,
            &borsh::to_vec(&DoubleZeroInstruction::CreateUser(
                self.create_user_args(CLIENT_IP, proof),
            ))
            .unwrap(),
            accounts,
        ));

        let payer = self.payer.insecure_clone();
        process_instructions(self.banks(), &instructions, &payer).await
    }

    /// The happy path: a genuine proof, its Ed25519 instruction, and the sysvar.
    async fn create_user_with_valid_proof(&mut self) -> Result<(), BanksClientError> {
        let proof = self.valid_proof();
        let prelude = [ed25519_instruction(
            &self.verifier.pubkey(),
            &proof.signature,
            &proof.signed_message(),
        )];
        self.create_user(Some(proof), &prelude, true).await
    }

    /// Turns `FeatureFlag::RequireIpOwnershipProof` on.
    async fn require_proof(&mut self) {
        let payer = self.payer.insecure_clone();
        let (program_id, globalstate) = (self.program_id, self.globalstate);
        let blockhash = wait_for_new_blockhash(self.banks()).await;
        execute_transaction(
            self.banks(),
            blockhash,
            program_id,
            DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
                feature_flags: FeatureFlag::RequireIpOwnershipProof.to_mask(),
            }),
            vec![AccountMeta::new(globalstate, false)],
            &payer,
        )
        .await;
    }

    /// Points `ip_verifier_authority_pk` at `pubkey` — including `Pubkey::default()`, to model an
    /// environment where no verifier has been configured yet.
    async fn set_verifier(&mut self, pubkey: Pubkey) {
        let payer = self.payer.insecure_clone();
        let (program_id, globalstate) = (self.program_id, self.globalstate);
        let blockhash = wait_for_new_blockhash(self.banks()).await;
        execute_transaction(
            self.banks(),
            blockhash,
            program_id,
            DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
                ip_verifier_authority_pk: Some(pubkey),
                ..Default::default()
            }),
            vec![AccountMeta::new(globalstate, false)],
            &payer,
        )
        .await;
    }

    /// Makes the fixture's payer the sentinel authority, the identity the shred-oracle signs with.
    async fn set_sentinel_authority(&mut self, pubkey: Pubkey) {
        let payer = self.payer.insecure_clone();
        let (program_id, globalstate) = (self.program_id, self.globalstate);
        let blockhash = wait_for_new_blockhash(self.banks()).await;
        execute_transaction(
            self.banks(),
            blockhash,
            program_id,
            DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
                sentinel_authority_pk: Some(pubkey),
                ..Default::default()
            }),
            vec![AccountMeta::new(globalstate, false)],
            &payer,
        )
        .await;
    }

    /// Provisions the AccessPass and multicast sub-allowlist for a custom `owner`, the way the
    /// shred-oracle flow does before it creates a validator-owned user. Returns the AccessPass PDA,
    /// which is keyed on the owner rather than the payer.
    async fn provision_owner(&mut self, owner: Pubkey, client_ip: Ipv4Addr) -> Pubkey {
        let program_id = self.program_id;
        let globalstate = self.globalstate;
        let mgroup = self.mgroup;
        let payer = self.payer.insecure_clone();
        let blockhash = self.context.last_blockhash;

        let (accesspass, _) = get_accesspass_pda(&program_id, &client_ip, &owner);
        execute_transaction(
            self.banks(),
            blockhash,
            program_id,
            DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                accesspass_type: AccessPassType::Prepaid,
                client_ip,
                last_access_epoch: 9999,
                allow_multiple_ip: false,
                max_unicast_users: 4,
                max_multicast_users: 4,
            }),
            vec![
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(owner, false),
            ],
            &payer,
        )
        .await;

        execute_transaction(
            self.banks(),
            blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip,
                    user_payer: owner,
                },
            ),
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
            ],
            &payer,
        )
        .await;

        accesspass
    }

    /// `CreateSubscribeUser` with an explicit `owner`, carrying `proof`. The AccessPass account is
    /// passed in because the override path keys it on the owner, not on `self.payer`.
    async fn create_subscribe_with_owner(
        &mut self,
        owner: Pubkey,
        accesspass: Pubkey,
        client_ip: Ipv4Addr,
        proof: Option<IpOwnershipProof>,
    ) -> Result<(), BanksClientError> {
        let user = self.user_pda(client_ip, UserType::Multicast);
        let mut accounts = self.create_subscribe_accounts(user);
        // The fixture's own AccessPass is keyed on the payer; swap in the owner's.
        accounts[3] = AccountMeta::new(accesspass, false);
        accounts.push(AccountMeta::new(self.payer.pubkey(), true));
        accounts.push(AccountMeta::new(
            solana_system_interface::program::ID,
            false,
        ));

        let prelude: Vec<_> = proof
            .as_ref()
            .map(|p| {
                vec![ed25519_instruction(
                    &self.verifier.pubkey(),
                    &p.signature,
                    &p.signed_message(),
                )]
            })
            .unwrap_or_default();
        if proof.is_some() {
            accounts.push(instructions_sysvar_meta());
        }

        let payer = self.payer.insecure_clone();
        let program_id = self.program_id;
        process_transaction_with_prelude(
            self.banks(),
            program_id,
            &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
                user_type: UserType::Multicast,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip,
                publisher: false,
                subscriber: true,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                owner,
                ip_proof: proof,
                extra_group_count: 0,
            }),
            &accounts,
            &payer,
            &prelude,
        )
        .await
    }

    async fn user_exists(&mut self, client_ip: Ipv4Addr, user_type: UserType) -> bool {
        let pda = self.user_pda(client_ip, user_type);
        get_account_data(self.banks(), pda)
            .await
            .map(|d| matches!(d, AccountData::User(_)))
            .unwrap_or(false)
    }
}

/// Asserts a rejection carried exactly the expected program error, so a test cannot pass because
/// creation failed for some unrelated reason.
#[track_caller]
fn assert_rejected(result: Result<(), BanksClientError>, expected: DoubleZeroError) {
    let err = result.expect_err("expected the transaction to be rejected");
    let expected_code = match solana_program::program_error::ProgramError::from(expected.clone()) {
        solana_program::program_error::ProgramError::Custom(code) => code,
        other => panic!("{expected:?} is not a custom error: {other:?}"),
    };
    assert_eq!(
        custom_error_code(&err),
        Some(expected_code),
        "expected {expected:?} (Custom({expected_code})), got: {err:?}"
    );
}

/// Brings up a program instance with global state, config, an activated device, an access pass for
/// `CLIENT_IP`, a multicast group, and a verifier key in global state. The feature flag is left
/// off; tests that want enforcement call `require_proof()`.
///
/// The access pass is deliberately a *specific-IP* pass rather than a wildcard: the flag makes the
/// proof mandatory for both, and a specific-IP pass is the case where it is easiest to accidentally
/// skip the check as redundant.
async fn setup() -> Fixture {
    let program_id = Pubkey::new_unique();
    let program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    let mut context = program_test.start_with_context().await;
    let payer = context.payer.insecure_clone();
    let recent_blockhash = context.last_blockhash;
    let banks_client = &mut context.banks_client;

    let (globalstate, _) = get_globalstate_pda(&program_id);
    let (globalconfig, _) = get_globalconfig_pda(&program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);

    init_globalstate_and_config(banks_client, program_id, &payer, recent_blockhash).await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (location, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (exchange, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (contributor, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "test".to_string(),
        }),
        vec![
            AccountMeta::new(contributor, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (device, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device, 0));
    let (dz_prefix_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device, 0));
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "test-dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "110.1.0.0/24".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(location, false),
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(location, false),
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(banks_client, globalstate).await;
    let (mgroup, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "mg1".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    let (accesspass, _) = get_accesspass_pda(&program_id, &CLIENT_IP, &payer.pubkey());
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: CLIENT_IP,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 4,
            max_multicast_users: 4,
        }),
        vec![
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: CLIENT_IP,
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
        ],
        &payer,
    )
    .await;

    let verifier = Keypair::new();
    let mut fixture = Fixture {
        context,
        payer,
        program_id,
        globalstate,
        device,
        accesspass,
        mgroup,
        verifier,
        resources: Resources {
            user_tunnel_block,
            multicast_publisher_block,
            tunnel_ids,
            dz_prefix_block,
        },
    };
    let verifier_pubkey = fixture.verifier.pubkey();
    fixture.set_verifier(verifier_pubkey).await;
    // `InitGlobalState` seeds `sentinel_authority_pk` to whoever initialized it, which here is the
    // fixture's payer — and the sentinel is exempt from the proof requirement. Rotating it away
    // means every test below exercises an ordinary, non-exempt payer; the sentinel tests set it
    // back deliberately.
    fixture.set_sentinel_authority(Pubkey::new_unique()).await;
    fixture
}

// ============================================================================
// Flag on — the proof is required
// ============================================================================

#[tokio::test]
async fn test_valid_proof_is_accepted() {
    let mut f = setup().await;
    f.require_proof().await;

    f.create_user_with_valid_proof()
        .await
        .expect("a genuine proof must be accepted");
    assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_valid_proof_is_accepted_for_create_subscribe_user() {
    let mut f = setup().await;
    f.require_proof().await;

    // CreateSubscribeUser derives the same User PDA, so the proof binds identically — the point of
    // routing both through create_user_core is that the two cannot drift.
    let user = f.user_pda(CLIENT_IP, UserType::Multicast);
    let proof = f.proof(&f.payer.pubkey(), CLIENT_IP, 0, UserType::Multicast);

    let mut accounts = f.create_subscribe_accounts(user);
    accounts.push(AccountMeta::new(f.payer.pubkey(), true));
    accounts.push(AccountMeta::new(
        solana_system_interface::program::ID,
        false,
    ));
    accounts.push(instructions_sysvar_meta());

    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];
    let payer = f.payer.insecure_clone();
    let program_id = f.program_id;
    process_transaction_with_prelude(
        f.banks(),
        program_id,
        &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: CLIENT_IP,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
            ip_proof: Some(proof),
            extra_group_count: 0,
        }),
        &accounts,
        &payer,
        &prelude,
    )
    .await
    .expect("a genuine proof must be accepted on CreateSubscribeUser too");

    assert!(f.user_exists(CLIENT_IP, UserType::Multicast).await);
}

#[tokio::test]
async fn test_create_subscribe_user_without_proof_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let user = f.user_pda(CLIENT_IP, UserType::Multicast);
    let mut accounts = f.create_subscribe_accounts(user);
    accounts.push(AccountMeta::new(f.payer.pubkey(), true));
    accounts.push(AccountMeta::new(
        solana_system_interface::program::ID,
        false,
    ));

    let payer = f.payer.insecure_clone();
    let program_id = f.program_id;
    let result = process_transaction_with_prelude(
        f.banks(),
        program_id,
        &DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: CLIENT_IP,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
            ip_proof: None,
            extra_group_count: 0,
        }),
        &accounts,
        &payer,
        &[],
    )
    .await;

    assert_rejected(result, DoubleZeroError::IpOwnershipProofRequired);
}

#[tokio::test]
async fn test_missing_proof_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let result = f.create_user(None, &[], false).await;
    assert_rejected(result, DoubleZeroError::IpOwnershipProofRequired);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_unset_verifier_key_rejects_rather_than_accepting_anything() {
    let mut f = setup().await;
    f.require_proof().await;
    // An environment where the flag was turned on before a verifier was configured. The default
    // key must be a hard stop: treating "no verifier" as "no check" would be strictly worse than
    // leaving the flag off, because it looks enforced.
    f.set_verifier(Pubkey::default()).await;

    let result = f.create_user_with_valid_proof().await;
    assert_rejected(result, DoubleZeroError::IpVerifierNotConfigured);
}

#[tokio::test]
async fn test_proof_for_a_different_payer_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // Genuinely signed, just not for this payer. The verification service signs the payer it is
    // told, without authenticating it, so this check is what stops a proof minted under someone
    // else's name from being usable.
    let proof = f.proof(&Pubkey::new_unique(), CLIENT_IP, 0, UserType::IBRL);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofPayerMismatch);
}

#[tokio::test]
async fn test_proof_for_a_different_client_ip_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let payer = f.payer.pubkey();
    let proof = f.proof(&payer, OTHER_IP, 0, UserType::IBRL);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofClientIpMismatch);
}

#[tokio::test]
async fn test_proof_for_a_different_user_type_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // The binding that stops a proof obtained for a routine connect being replayed into an
    // operation on a different account inside the same epoch (#4192 item 2). The User PDA is
    // `f(client_ip, user_type)`, so the proof here is for the Multicast user at the same IP and
    // the request is for the IBRL one — a different account.
    let payer = f.payer.pubkey();
    let proof = f.proof(&payer, CLIENT_IP, 0, UserType::Multicast);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofUserTypeMismatch);
}

#[tokio::test]
async fn test_owner_override_proof_binds_the_owner_not_the_payer() {
    let mut f = setup().await;
    f.require_proof().await;

    // The shred-oracle shape: the sentinel or a USER_ADMIN holder pays, and the user it creates is
    // owned by a validator. The AccessPass is keyed on that owner, and it is the owner who
    // operates `client_ip` — so the owner is who the proof must name. A proof naming the payer
    // could never be obtained for an address the payer does not operate.
    let owner = Pubkey::new_unique();
    let accesspass = f.provision_owner(owner, CLIENT_IP).await;
    let proof = f.proof(&owner, CLIENT_IP, 0, UserType::Multicast);

    f.create_subscribe_with_owner(owner, accesspass, CLIENT_IP, Some(proof))
        .await
        .expect("a proof naming the effective owner must be accepted on the override path");

    assert!(f.user_exists(CLIENT_IP, UserType::Multicast).await);
}

#[tokio::test]
async fn test_owner_override_proof_naming_the_payer_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // The mirror image, and the one that would silently pass if the binding used the transaction
    // payer: a proof for the *payer* says nothing about who controls the address the user is
    // being created for.
    let owner = Pubkey::new_unique();
    let accesspass = f.provision_owner(owner, CLIENT_IP).await;
    let proof = f.proof(&f.payer.pubkey(), CLIENT_IP, 0, UserType::Multicast);

    let result = f
        .create_subscribe_with_owner(owner, accesspass, CLIENT_IP, Some(proof))
        .await;
    assert_rejected(result, DoubleZeroError::IpProofPayerMismatch);
    assert!(!f.user_exists(CLIENT_IP, UserType::Multicast).await);
}

#[tokio::test]
async fn test_sentinel_payer_may_create_without_a_proof_while_the_flag_is_set() {
    let mut f = setup().await;
    f.require_proof().await;

    // The shred-oracle provisions multicast publishers owned by validators, so a proof would have
    // to name the validator for an address the oracle never observes a request from — there is no
    // proof it could obtain. The exemption is keyed on the sentinel authority, a
    // DoubleZero-operated key, so it is not a path a registrant can reach.
    let sentinel = f.payer.pubkey();
    f.set_sentinel_authority(sentinel).await;

    let owner = Pubkey::new_unique();
    let accesspass = f.provision_owner(owner, CLIENT_IP).await;

    f.create_subscribe_with_owner(owner, accesspass, CLIENT_IP, None)
        .await
        .expect("the sentinel authority is exempt from the proof requirement");

    assert!(f.user_exists(CLIENT_IP, UserType::Multicast).await);
}

#[tokio::test]
async fn test_sentinel_payer_still_has_a_supplied_proof_validated() {
    let mut f = setup().await;
    f.require_proof().await;

    // The exemption waives the requirement, not validation. Keeping a supplied proof checked is
    // what lets the oracle start carrying real proofs without a program change, and it means a
    // broken one is a visible error rather than a silent bypass.
    let sentinel = f.payer.pubkey();
    f.set_sentinel_authority(sentinel).await;

    let owner = Pubkey::new_unique();
    let accesspass = f.provision_owner(owner, CLIENT_IP).await;
    // Signed for the wrong IP.
    let proof = f.proof(&owner, OTHER_IP, 0, UserType::Multicast);

    let result = f
        .create_subscribe_with_owner(owner, accesspass, CLIENT_IP, Some(proof))
        .await;
    assert_rejected(result, DoubleZeroError::IpProofClientIpMismatch);
    assert!(!f.user_exists(CLIENT_IP, UserType::Multicast).await);
}

#[tokio::test]
async fn test_non_sentinel_payer_is_not_exempt() {
    let mut f = setup().await;
    f.require_proof().await;

    // The mirror of the exemption test: the fixture's sentinel authority is somebody else, so the
    // same creation from the same payer is rejected. Without this, "sentinel is exempt" could be
    // passing because the check is vacuous.
    let owner = Pubkey::new_unique();
    let accesspass = f.provision_owner(owner, CLIENT_IP).await;

    let result = f
        .create_subscribe_with_owner(owner, accesspass, CLIENT_IP, None)
        .await;
    assert_rejected(result, DoubleZeroError::IpOwnershipProofRequired);
}

#[tokio::test]
async fn test_unsupported_proof_version_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // A genuine signature over a layout this program cannot reconstruct. Accepting it would mean
    // rebuilding v1 bytes and comparing them against a message signed over something else, which
    // is exactly what the version byte exists to prevent — a v2 rolls out by teaching the program
    // the new layout first, not by letting an unknown version through.
    let proof = sign_version(
        IP_PROOF_VERSION + 1,
        &f.verifier,
        &f.payer.pubkey(),
        &CLIENT_IP,
        0,
        UserType::IBRL as u8,
    );
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofVersionUnsupported);
}

#[tokio::test]
async fn test_previous_epoch_is_accepted() {
    let mut f = setup().await;
    f.require_proof().await;

    // A client that fetched its proof moments before an epoch rollover must still be able to
    // connect; without the trailing epoch the window would be a cliff whose sharpness depends on
    // how long the transaction took to land.
    f.context.warp_to_epoch(1).expect("warp to epoch 1");
    assert_eq!(f.current_epoch().await, 1);

    let payer = f.payer.pubkey();
    let proof = f.proof(&payer, CLIENT_IP, 0, UserType::IBRL);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    f.create_user(Some(proof), &prelude, true)
        .await
        .expect("a proof from the previous epoch must still be accepted");
    assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_epoch_two_back_is_rejected_after_a_warp() {
    let mut f = setup().await;
    f.require_proof().await;

    // The stale case expressed as the window actually moving, rather than as an epoch number that
    // was never current: at epoch 2, a proof from epoch 0 has fallen out.
    f.context.warp_to_epoch(2).expect("warp to epoch 2");
    assert_eq!(f.current_epoch().await, 2);

    let payer = f.payer.pubkey();
    let proof = f.proof(&payer, CLIENT_IP, 0, UserType::IBRL);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofEpochOutOfWindow);
}

#[tokio::test]
async fn test_stale_and_future_epochs_are_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let payer = f.payer.pubkey();

    // ProgramTest starts at epoch 0, so `epoch - 2` is expressed as a large epoch that is neither
    // the current one nor its predecessor, and the future case as epoch 1.
    for epoch in [2, 5, u64::MAX] {
        let proof = f.proof(&payer, CLIENT_IP, epoch, UserType::IBRL);
        let prelude = [ed25519_instruction(
            &f.verifier.pubkey(),
            &proof.signature,
            &proof.signed_message(),
        )];
        let result = f.create_user(Some(proof), &prelude, true).await;
        assert_rejected(result, DoubleZeroError::IpProofEpochOutOfWindow);
    }

    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_signature_over_a_different_message_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let payer = f.payer.pubkey();

    // Every proof field agrees with the request, but the signature covers other bytes. The
    // precompile verifies whatever the instruction hands it, so the instruction here carries the
    // message that was actually signed — the program must notice it is not the message this
    // creation implies.
    let decoy = signed_message_for(IP_PROOF_VERSION, &payer, &OTHER_IP, 0, UserType::IBRL as u8);
    let mut proof = f.proof(&payer, OTHER_IP, 0, UserType::IBRL);
    proof.client_ip = CLIENT_IP;

    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &decoy,
    )];
    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofMessageMismatch);
}

#[tokio::test]
async fn test_proof_signed_by_the_wrong_key_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // A syntactically perfect proof from a key that is not the trust root — an old verifier after
    // a rotation, or an attacker's own key.
    let impostor = Keypair::new();
    let proof = sign(
        &impostor,
        &f.payer.pubkey(),
        &CLIENT_IP,
        0,
        UserType::IBRL as u8,
    );
    let prelude = [ed25519_instruction(
        &impostor.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofVerifierKeyMismatch);
}

#[tokio::test]
async fn test_missing_ed25519_instruction_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    // The proof is genuine; nothing verified it. Without this check the whole scheme reduces to
    // trusting instruction data.
    let proof = f.valid_proof();
    let result = f.create_user(Some(proof), &[], true).await;
    assert_rejected(result, DoubleZeroError::IpProofEd25519InstructionMissing);
}

#[tokio::test]
async fn test_missing_instructions_sysvar_account_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];
    // The Ed25519 instruction is present, but without the sysvar the program cannot see it.
    let result = f.create_user(Some(proof), &prelude, false).await;
    assert_rejected(result, DoubleZeroError::IpProofInstructionsSysvarMissing);
}

#[tokio::test]
async fn test_signature_mismatch_between_proof_and_instruction_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    // The precompile verified a real signature over the right message with the right key — just
    // not the signature the proof carries. Left unchecked, the proof's own signature field would
    // be decorative.
    let other = sign(
        &f.verifier,
        &f.payer.pubkey(),
        &OTHER_IP,
        0,
        UserType::IBRL as u8,
    );
    let mut tampered = proof;
    tampered.signature = other.signature;

    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];
    let result = f.create_user(Some(tampered), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofSignatureMismatch);
}

#[tokio::test]
async fn test_ed25519_instruction_is_found_at_any_index() {
    // The client is free to interleave compute-budget instructions, so the program scans rather
    // than reading a fixed offset. Each iteration starts from a clean fixture because success
    // creates the user.
    // Distinct budget instruction kinds: the runtime rejects a transaction carrying two of the
    // same one, so the index cannot be varied by repeating a single instruction.
    let budget_instructions = [
        ComputeBudgetInstruction::set_compute_unit_limit(1_400_000),
        ComputeBudgetInstruction::set_compute_unit_price(1),
    ];
    for leading_budget_instructions in 0..=budget_instructions.len() {
        let mut f = setup().await;
        f.require_proof().await;

        let proof = f.valid_proof();
        let mut prelude: Vec<solana_sdk::instruction::Instruction> =
            budget_instructions[..leading_budget_instructions].to_vec();
        prelude.push(ed25519_instruction(
            &f.verifier.pubkey(),
            &proof.signature,
            &proof.signed_message(),
        ));

        f.create_user(Some(proof), &prelude, true)
            .await
            .unwrap_or_else(|e| {
                panic!(
                    "Ed25519 instruction at index {leading_budget_instructions} not found: {e:?}"
                )
            });
        assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
    }
}

#[tokio::test]
async fn test_ed25519_instruction_after_the_program_instruction_is_found() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    let user = f.user_pda(CLIENT_IP, UserType::IBRL);
    let mut accounts = f.create_user_accounts(user);
    accounts.push(AccountMeta::new(f.payer.pubkey(), true));
    accounts.push(AccountMeta::new(
        solana_system_interface::program::ID,
        false,
    ));
    accounts.push(instructions_sysvar_meta());

    // The sysvar exposes the whole transaction, not just what came before, so a trailing Ed25519
    // instruction is equally valid — and the precompile still runs.
    let instructions = vec![
        solana_sdk::instruction::Instruction::new_with_bytes(
            f.program_id,
            &borsh::to_vec(&DoubleZeroInstruction::CreateUser(
                f.create_user_args(CLIENT_IP, Some(proof)),
            ))
            .unwrap(),
            accounts,
        ),
        ed25519_instruction(
            &f.verifier.pubkey(),
            &proof.signature,
            &proof.signed_message(),
        ),
    ];

    let payer = f.payer.insecure_clone();
    process_instructions(f.banks(), &instructions, &payer)
        .await
        .expect("a trailing Ed25519 instruction must be found");
    assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

// The three malformed-precompile shapes below — offsets naming another instruction, offsets past
// the end of the data, and a multi-signature instruction — are rejected by the *runtime* before the
// serviceability program is ever entered, because the precompile validates its own layout. That is
// worth asserting: it is the outer layer of the defence. The program's own checks, which must hold
// even if that layer ever loosens, are unit-tested directly against `check_ed25519_instruction` in
// `src/ip_proof.rs`, where arbitrary bytes can be fed in.

/// Every rejection here is a precompile failure, not a program failure — the assertion is that the
/// transaction does not land, and that the user is not created.
#[track_caller]
fn assert_not_a_program_rejection(result: Result<(), BanksClientError>) {
    let err = result.expect_err("expected the transaction to be rejected");
    assert!(
        custom_error_code(&err).is_none_or(|code| code < 100),
        "expected a runtime/precompile rejection, not a serviceability error: {err:?}"
    );
}

#[tokio::test]
async fn test_offsets_pointing_at_another_instruction_do_not_land() {
    let mut f = setup().await;
    f.require_proof().await;

    // The dangerous shape: the precompile reads the message from a different instruction and
    // verifies a signature over *that*, while a naive program compares against this instruction's
    // own bytes and concludes the right message was signed.
    let proof = f.valid_proof();
    let message = proof.signed_message();
    let carrier = solana_sdk::instruction::Instruction {
        program_id: Pubkey::new_unique(),
        accounts: vec![],
        data: message.to_vec(),
    };
    let mut ed25519 = ed25519_instruction(&f.verifier.pubkey(), &proof.signature, &message);
    // Repoint message_instruction_index (the 7th u16, at byte 14 of the offsets block) at
    // instruction 0, leaving the bytes themselves intact so only the provenance changes.
    ed25519.data[14..16].copy_from_slice(&0u16.to_le_bytes());

    let result = f.create_user(Some(proof), &[carrier, ed25519], true).await;
    assert_not_a_program_rejection(result);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_offsets_pointing_outside_the_instruction_do_not_land() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    let mut ed25519 = ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    );
    // Push message_data_offset past the end of the data.
    ed25519.data[10..12].copy_from_slice(&u16::MAX.to_le_bytes());

    let result = f.create_user(Some(proof), &[ed25519], true).await;
    assert_not_a_program_rejection(result);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_multi_signature_ed25519_instruction_does_not_land() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    // Claims two signatures while carrying the bytes of one.
    let ed25519 = ed25519_instruction_raw(
        2,
        f.verifier.pubkey().as_ref(),
        &proof.signature,
        &proof.signed_message(),
        u16::MAX,
        u16::MAX,
        u16::MAX,
    );

    let result = f.create_user(Some(proof), &[ed25519], true).await;
    assert_not_a_program_rejection(result);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_truncated_ed25519_instruction_does_not_land() {
    let mut f = setup().await;
    f.require_proof().await;

    let proof = f.valid_proof();
    // Shorter than the 16-byte header, so there are no offsets to read at all.
    let stub = solana_sdk::instruction::Instruction {
        program_id: solana_sdk_ids::ed25519_program::id(),
        accounts: vec![],
        data: vec![1, 0, 0],
    };

    let result = f.create_user(Some(proof), &[stub], true).await;
    assert_not_a_program_rejection(result);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

// ============================================================================
// The idempotent rerun path
// ============================================================================

#[tokio::test]
async fn test_rerun_without_a_proof_is_rejected() {
    let mut f = setup().await;
    f.require_proof().await;

    f.create_user_with_valid_proof()
        .await
        .expect("initial creation");

    // create_user_core returns early for an existing, matching user. Validating the proof before
    // that return is what stops an account squatted earlier from absorbing every later call
    // without its holder ever demonstrating control of the address.
    let result = f.create_user(None, &[], false).await;
    assert_rejected(result, DoubleZeroError::IpOwnershipProofRequired);
}

#[tokio::test]
async fn test_rerun_with_a_valid_proof_is_still_a_no_op() {
    let mut f = setup().await;
    f.require_proof().await;

    f.create_user_with_valid_proof()
        .await
        .expect("initial creation");
    let user_pda = f.user_pda(CLIENT_IP, UserType::IBRL);
    let before = get_account_data(f.banks(), user_pda).await.unwrap();

    f.create_user_with_valid_proof()
        .await
        .expect("a rerun carrying a proof must remain idempotent");

    let after = get_account_data(f.banks(), user_pda).await.unwrap();
    assert_eq!(before, after, "the rerun must not have mutated the user");
}

// ============================================================================
// Flag off — a proof is optional but never merely decorative
// ============================================================================

#[tokio::test]
async fn test_creation_without_a_proof_succeeds_while_the_flag_is_off() {
    let mut f = setup().await;

    f.create_user(None, &[], false)
        .await
        .expect("pre-RFC-27 clients must keep working until the flag is set");
    assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_valid_proof_is_accepted_while_the_flag_is_off() {
    let mut f = setup().await;

    f.create_user_with_valid_proof()
        .await
        .expect("a client that upgraded early must not be penalised for it");
    assert!(f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_invalid_proof_is_rejected_even_while_the_flag_is_off() {
    let mut f = setup().await;

    // A client that attaches a broken proof is broken now, not at rollout. Letting it through
    // would hide the breakage until the flag flips, which is when it is most expensive to find.
    let proof = f.proof(&Pubkey::new_unique(), CLIENT_IP, 0, UserType::IBRL);
    let prelude = [ed25519_instruction(
        &f.verifier.pubkey(),
        &proof.signature,
        &proof.signed_message(),
    )];

    let result = f.create_user(Some(proof), &prelude, true).await;
    assert_rejected(result, DoubleZeroError::IpProofPayerMismatch);
    assert!(!f.user_exists(CLIENT_IP, UserType::IBRL).await);
}

#[tokio::test]
async fn test_proof_without_its_ed25519_instruction_is_rejected_while_the_flag_is_off() {
    let mut f = setup().await;

    let proof = f.valid_proof();
    let result = f.create_user(Some(proof), &[], true).await;
    assert_rejected(result, DoubleZeroError::IpProofEd25519InstructionMissing);
}
