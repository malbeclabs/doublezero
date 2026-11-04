//! Every processor must reject a global state account that is not the program's global state PDA.
//! Each case signs as an outsider who is in no allowlist, and passes a copy of the real global
//! state that names the outsider in `foundation_allowlist`, in `qa_allowlist` and as the sentinel.

mod test_helpers;

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_contributor_pda, get_device_pda, get_link_pda, get_location_pda,
        get_multicastgroup_pda, get_permission_pda, get_resource_extension_pda, get_tenant_pda,
        get_user_pda,
    },
    processors::{
        accesspass::{
            check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs,
            set::SetAccessPassArgs, set_feeds::SetAccessPassFeedsArgs,
            set_flags::SetAccessPassFlagsArgs,
        },
        allowlist::{
            foundation::{add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs},
            qa::{add::AddQaAllowlistArgs, remove::RemoveQaAllowlistArgs},
        },
        contributor::create::ContributorCreateArgs,
        device::{
            create::DeviceCreateArgs, interface::create::DeviceInterfaceCreateArgs,
            update::DeviceUpdateArgs,
        },
        globalstate::{setauthority::SetAuthorityArgs, setfeatureflags::SetFeatureFlagsArgs},
        link::{accept::LinkAcceptArgs, create::LinkCreateArgs},
        location::create::LocationCreateArgs,
        multicastgroup::{
            allowlist::{
                publisher::{
                    add::AddMulticastGroupPubAllowlistArgs,
                    remove::RemoveMulticastGroupPubAllowlistArgs,
                },
                subscriber::{
                    add::AddMulticastGroupSubAllowlistArgs,
                    remove::RemoveMulticastGroupSubAllowlistArgs,
                },
            },
            create::MulticastGroupCreateArgs,
            delete::MulticastGroupDeleteArgs,
            subscribe::UpdateMulticastGroupRolesArgs,
        },
        permission::{create::PermissionCreateArgs, update::PermissionUpdateArgs},
        tenant::{add_administrator::TenantAddAdministratorArgs, create::TenantCreateArgs},
        user::{
            create::UserCreateArgs, create_subscribe::UserCreateSubscribeArgs,
            delete::UserDeleteArgs,
        },
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassType, FeedSeat},
        device::{DeviceDesiredStatus, DeviceType},
        feature_flags::FeatureFlag,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{LinkDesiredStatus, LinkLinkType},
        permission::permission_flags,
        user::{UserCYOA, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::{Account, AccountSharedData},
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    rent::Rent,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};
use std::net::Ipv4Addr;
use test_helpers::*;

const IP: Ipv4Addr = Ipv4Addr::new(100, 0, 0, 10);

#[derive(Clone, Copy)]
enum Owner {
    Program,
    /// Owned by another program, here the loader.
    Loader,
}

enum Expect {
    /// Rejected by `GlobalState::load`.
    Load,
    /// Asserts the owner before loading, so a loader-owned copy panics there first.
    OwnerAsserted,
    /// Asserts the PDA before loading, so every copy panics there first.
    Rejected,
}

struct Case {
    ix: DoubleZeroInstruction,
    /// As a legitimate caller would send them, with `Env::copy` in the global state slot.
    accounts: Vec<AccountMeta>,
    /// The accounts the instruction would change if it accepted the copy.
    watched: Vec<Pubkey>,
    /// How the real global state refuses the outsider.
    refused: DoubleZeroError,
}

struct Dev {
    pk: Pubkey,
    tunnel_ids: Pubkey,
    dz_prefix: Pubkey,
}

struct Env {
    ctx: ProgramTestContext,
    program_id: Pubkey,
    /// A foundation member, per `InitGlobalState`.
    payer: Keypair,
    outsider: Keypair,
    gs: Pubkey,
    gc: Pubkey,
    /// Where the copied global state is seeded, after the case has set up.
    copy: Pubkey,
}

impl Env {
    async fn new() -> Self {
        let (mut ctx, program_id, gs, gc) = setup_program_with_globalconfig_context().await;
        let payer = ctx.payer.insecure_clone();
        let outsider = Keypair::new();
        transfer(
            &mut ctx.banks_client,
            &payer,
            &outsider.pubkey(),
            10_000_000_000,
        )
        .await;
        Self {
            ctx,
            program_id,
            payer,
            outsider,
            gs,
            gc,
            copy: Pubkey::new_unique(),
        }
    }

    /// Runs `ix` as the foundation member.
    async fn exec(&mut self, ix: DoubleZeroInstruction, accounts: Vec<AccountMeta>) {
        execute_transaction(
            &mut self.ctx.banks_client,
            self.ctx.last_blockhash,
            self.program_id,
            ix,
            accounts,
            &self.payer,
        )
        .await;
    }

    async fn next_index(&mut self) -> u128 {
        get_globalstate(&mut self.ctx.banks_client, self.gs)
            .await
            .account_index
            + 1
    }

    fn resource(&self, resource_type: ResourceType) -> Pubkey {
        get_resource_extension_pda(&self.program_id, resource_type).0
    }

    /// `accounts`, then the outsider as signing payer and the system program.
    fn signed(&self, mut accounts: Vec<AccountMeta>) -> Vec<AccountMeta> {
        accounts.push(AccountMeta::new(self.outsider.pubkey(), true));
        accounts.push(AccountMeta::new_readonly(
            solana_system_interface::program::ID,
            false,
        ));
        accounts
    }

    fn gs_meta(&self) -> AccountMeta {
        AccountMeta::new(self.copy, false)
    }

    /// Location, exchange and contributor (owned by the foundation member).
    async fn site(&mut self) -> (Pubkey, Pubkey, Pubkey) {
        let payer = self.payer.insecure_clone();
        setup_device_prerequisites(
            &mut self.ctx.banks_client,
            self.ctx.last_blockhash,
            self.program_id,
            self.gs,
            self.gc,
            &payer,
        )
        .await
    }

    async fn device(
        &mut self,
        (location, exchange, contributor): (Pubkey, Pubkey, Pubkey),
        code: &str,
        public_ip: [u8; 4],
        dz_prefixes: &str,
        max_users: u16,
    ) -> Dev {
        let index = self.next_index().await;
        let (pk, _) = get_device_pda(&self.program_id, index);
        let tunnel_ids = self.resource(ResourceType::TunnelIds(pk, 0));
        let dz_prefix = self.resource(ResourceType::DzPrefixBlock(pk, 0));
        self.exec(
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                code: code.to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: public_ip.into(),
                dz_prefixes: dz_prefixes.parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
                mgmt_vrf: "mgmt".to_string(),
                desired_status: Some(DeviceDesiredStatus::Activated),
                resource_count: 2,
            }),
            vec![
                AccountMeta::new(pk, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(location, false),
                AccountMeta::new(exchange, false),
                AccountMeta::new(self.gs, false),
                AccountMeta::new(self.gc, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz_prefix, false),
            ],
        )
        .await;
        if max_users > 0 {
            self.exec(
                DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    max_users: Some(max_users),
                    ..DeviceUpdateArgs::default()
                }),
                vec![
                    AccountMeta::new(pk, false),
                    AccountMeta::new(contributor, false),
                    AccountMeta::new(location, false),
                    AccountMeta::new(location, false),
                    AccountMeta::new(self.gs, false),
                ],
            )
            .await;
        }
        Dev {
            pk,
            tunnel_ids,
            dz_prefix,
        }
    }

    /// One activated device; a `max_users` of 0 leaves it locked.
    async fn network(&mut self, max_users: u16) -> Dev {
        let site = self.site().await;
        self.device(site, "dev", [100, 0, 0, 1], "110.1.0.0/24", max_users)
            .await
    }

    /// The accounts user creation and deletion allocate from, in instruction order.
    fn user_resources(&self, dev: &Dev) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(self.resource(ResourceType::UserTunnelBlock), false),
            AccountMeta::new(self.resource(ResourceType::MulticastPublisherBlock), false),
            AccountMeta::new(dev.tunnel_ids, false),
            AccountMeta::new(dev.dz_prefix, false),
        ]
    }

    /// A multicast group owned by the foundation member.
    async fn mgroup(&mut self) -> Pubkey {
        let index = self.next_index().await;
        let (pk, _) = get_multicastgroup_pda(&self.program_id, index);
        let block = self.resource(ResourceType::MulticastGroupBlock);
        self.exec(
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                code: "group".to_string(),
                max_bandwidth: 1000,
                owner: self.payer.pubkey(),
                use_onchain_allocation: true,
                bond_ref: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(pk, false),
                AccountMeta::new(self.gs, false),
                AccountMeta::new(block, false),
            ],
        )
        .await;
        pk
    }

    async fn access_pass(&mut self, user_payer: Pubkey, accesspass_type: AccessPassType) -> Pubkey {
        let (pk, _) = get_accesspass_pda(&self.program_id, &IP, &user_payer);
        self.exec(
            DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                accesspass_type,
                client_ip: IP,
                last_access_epoch: 9999,
                allow_multiple_ip: false,
                max_unicast_users: 1,
                max_multicast_users: 1,
            }),
            vec![
                AccountMeta::new(pk, false),
                AccountMeta::new(self.gs, false),
                AccountMeta::new(user_payer, false),
            ],
        )
        .await;
        pk
    }

    async fn allowlist(
        &mut self,
        mgroup: Pubkey,
        pass: Pubkey,
        user_payer: Pubkey,
        publisher: bool,
    ) {
        let ix = if publisher {
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip: IP,
                    user_payer,
                },
            )
        } else {
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: IP,
                    user_payer,
                },
            )
        };
        self.exec(
            ix,
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(pass, false),
                AccountMeta::new(self.gs, false),
            ],
        )
        .await;
    }

    /// An activated IBRL user the foundation member owns. Returns (access pass, user).
    async fn user(&mut self, dev: &Dev) -> (Pubkey, Pubkey) {
        let pass = self
            .access_pass(self.payer.pubkey(), AccessPassType::Prepaid)
            .await;
        let (user, _) = get_user_pda(&self.program_id, &IP, UserType::IBRL);
        let mut accounts = vec![
            AccountMeta::new(user, false),
            AccountMeta::new(dev.pk, false),
            AccountMeta::new(pass, false),
            AccountMeta::new(self.gs, false),
        ];
        accounts.extend(self.user_resources(dev));
        self.exec(
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                user_type: UserType::IBRL,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: IP,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                dz_prefix_count: 1,
                ip_proof: None,
            }),
            accounts,
        )
        .await;
        (pass, user)
    }

    async fn permission(&mut self, user_payer: Pubkey, permissions: u128) -> Pubkey {
        let (pk, _) = get_permission_pda(&self.program_id, &user_payer);
        self.exec(
            DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
                user_payer,
                permissions,
            }),
            vec![
                AccountMeta::new(pk, false),
                AccountMeta::new(self.gs, false),
            ],
        )
        .await;
        pk
    }

    async fn interface(&mut self, dev: &Dev, contributor: Pubkey, name: &str) {
        let tunnel_block = self.resource(ResourceType::DeviceTunnelBlock);
        let segment_ids = self.resource(ResourceType::SegmentRoutingIds);
        self.exec(
            DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
                name: name.to_string(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 100_000_000_000,
                ip_net: None,
                cir: 0,
                mtu: 9000,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
                use_onchain_allocation: true,
                topology_count: 0,
            }),
            vec![
                AccountMeta::new(dev.pk, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(self.gs, false),
                AccountMeta::new(tunnel_block, false),
                AccountMeta::new(segment_ids, false),
            ],
        )
        .await;
    }

    async fn rewrite_access_pass(&mut self, pk: Pubkey, edit: impl FnOnce(&mut AccessPass)) {
        let mut account = self
            .ctx
            .banks_client
            .get_account(pk)
            .await
            .unwrap()
            .expect("access pass must exist");
        let mut pass = AccessPass::try_from(&account.data[..]).unwrap();
        edit(&mut pass);
        account.data = borsh::to_vec(&pass).unwrap();
        account.lamports = Rent::default().minimum_balance(account.data.len());
        self.ctx.set_account(&pk, &AccountSharedData::from(account));
    }

    /// The real global state, granting the outsider foundation, QA and sentinel authority.
    /// `feed_authority_pk` is left alone: it would confine the outsider to passes it owns.
    async fn seed_copy(&mut self, owner: Owner) {
        let mut gs = get_globalstate(&mut self.ctx.banks_client, self.gs).await;
        let outsider = self.outsider.pubkey();
        gs.foundation_allowlist.push(outsider);
        gs.qa_allowlist.push(outsider);
        gs.sentinel_authority_pk = outsider;
        let data = borsh::to_vec(&gs).unwrap();
        let owner = match owner {
            Owner::Program => self.program_id,
            Owner::Loader => solana_sdk_ids::bpf_loader_upgradeable::id(),
        };
        self.ctx.set_account(
            &self.copy,
            &AccountSharedData::from(Account {
                lamports: Rent::default().minimum_balance(data.len()),
                data,
                owner,
                executable: false,
                rent_epoch: 0,
            }),
        );
    }

    async fn snapshot(&mut self, keys: &[Pubkey]) -> Vec<Option<Account>> {
        let mut accounts = Vec::with_capacity(keys.len());
        for key in keys {
            accounts.push(self.ctx.banks_client.get_account(*key).await.unwrap());
        }
        accounts
    }

    /// Sends `case` as the outsider, with `globalstate` in the global state slot.
    async fn send(&mut self, case: &Case, globalstate: Pubkey) -> Result<(), BanksClientError> {
        let accounts = case
            .accounts
            .iter()
            .map(|meta| {
                let pubkey = if meta.pubkey == self.copy {
                    globalstate
                } else {
                    meta.pubkey
                };
                AccountMeta { pubkey, ..*meta }
            })
            .collect();
        let ix = Instruction::new_with_bytes(
            self.program_id,
            &borsh::to_vec(&case.ix).unwrap(),
            accounts,
        );
        let blockhash = self.ctx.banks_client.get_latest_blockhash().await.unwrap();
        let tx = Transaction::new_signed_with_payer(
            &[ix],
            Some(&self.outsider.pubkey()),
            &[&self.outsider],
            blockhash,
        );
        self.ctx.banks_client.process_transaction(tx).await
    }

    async fn check(mut self, case: Case, owner: Owner, expect: Expect) {
        let err = self
            .send(&case, self.gs)
            .await
            .expect_err("the real global state must refuse the outsider");
        assert_custom_error(&err, case.refused.clone());

        self.seed_copy(owner).await;
        let watched = [case.watched.as_slice(), &[self.gs, self.copy]].concat();
        let before = self.snapshot(&watched).await;

        let result = self.send(&case, self.copy).await;
        let err = result.expect_err("the copied global state was accepted");
        let expected = match (expect, owner) {
            (Expect::Load | Expect::OwnerAsserted, Owner::Program) => {
                InstructionError::InvalidSeeds
            }
            (Expect::Load, Owner::Loader) => {
                InstructionError::Custom(custom_code(DoubleZeroError::InvalidAccountOwner))
            }
            (Expect::OwnerAsserted, Owner::Loader) | (Expect::Rejected, _) => {
                InstructionError::ProgramFailedToComplete
            }
        };
        assert_eq!(
            err.unwrap(),
            TransactionError::InstructionError(0, expected)
        );
        assert_eq!(
            self.snapshot(&watched).await,
            before,
            "a refused copy must change nothing"
        );
    }
}

// --- users -------------------------------------------------------------------------------------

/// QA membership lifts the device's capacity limit.
async fn create_user(env: &mut Env) -> Case {
    let dev = env.network(0).await;
    let pass = env
        .access_pass(env.outsider.pubkey(), AccessPassType::Prepaid)
        .await;
    let (user, _) = get_user_pda(&env.program_id, &IP, UserType::IBRL);
    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(dev.pk, false),
        AccountMeta::new(pass, false),
        env.gs_meta(),
    ];
    accounts.extend(env.user_resources(&dev));
    Case {
        ix: DoubleZeroInstruction::CreateUser(UserCreateArgs {
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: IP,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: None,
        }),
        accounts: env.signed(accounts),
        watched: vec![user, dev.pk, pass],
        refused: DoubleZeroError::MaxUsersExceeded,
    }
}

/// The foundation-only owner override: a user another key owns, made by the outsider.
async fn create_subscribe_user(env: &mut Env) -> Case {
    let dev = env.network(128).await;
    let mgroup = env.mgroup().await;
    let other_user = Pubkey::new_unique();
    let pass = env.access_pass(other_user, AccessPassType::Prepaid).await;
    env.allowlist(mgroup, pass, other_user, true).await;
    let (user, _) = get_user_pda(&env.program_id, &IP, UserType::Multicast);
    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(dev.pk, false),
        AccountMeta::new(mgroup, false),
        AccountMeta::new(pass, false),
        env.gs_meta(),
    ];
    accounts.extend(env.user_resources(&dev));
    Case {
        ix: DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: IP,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: other_user,
            ip_proof: None,
            extra_group_count: 0,
        }),
        accounts: env.signed(accounts),
        watched: vec![user, dev.pk, mgroup, pass],
        refused: DoubleZeroError::NotAllowed,
    }
}

async fn delete_user_case(env: &mut Env, ix: DoubleZeroInstruction) -> Case {
    let dev = env.network(128).await;
    let (pass, user) = env.user(&dev).await;
    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(pass, false),
        env.gs_meta(),
        AccountMeta::new(dev.pk, false),
    ];
    accounts.extend(env.user_resources(&dev));
    accounts.push(AccountMeta::new(env.payer.pubkey(), false));
    Case {
        ix,
        accounts: env.signed(accounts),
        watched: vec![user, pass, dev.pk],
        refused: DoubleZeroError::NotAllowed,
    }
}

const DELETE_ARGS: UserDeleteArgs = UserDeleteArgs {
    dz_prefix_count: 1,
    multicast_publisher_count: 1,
};

/// Deletes a user the foundation member owns.
async fn delete_user(env: &mut Env) -> Case {
    delete_user_case(env, DoubleZeroInstruction::DeleteUser(DELETE_ARGS)).await
}

async fn delete_prepaid_user(env: &mut Env) -> Case {
    delete_user_case(env, DoubleZeroInstruction::DeletePrepaidUser(DELETE_ARGS)).await
}

// --- multicast group allowlists ----------------------------------------------------------------

/// Creates a prepaid pass for an arbitrary user, already allowlisted on the group.
async fn add_allowlist_case(env: &mut Env, publisher: bool) -> Case {
    let mgroup = env.mgroup().await;
    let other_user = Pubkey::new_unique();
    let (pass, _) = get_accesspass_pda(&env.program_id, &IP, &other_user);
    let ix = if publisher {
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: IP,
            user_payer: other_user,
        })
    } else {
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: IP,
            user_payer: other_user,
        })
    };
    Case {
        ix,
        accounts: env.signed(vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(pass, false),
            env.gs_meta(),
        ]),
        watched: vec![pass, mgroup],
        refused: DoubleZeroError::NotAllowed,
    }
}

async fn add_mgroup_pub_allowlist(env: &mut Env) -> Case {
    add_allowlist_case(env, true).await
}

async fn add_mgroup_sub_allowlist(env: &mut Env) -> Case {
    add_allowlist_case(env, false).await
}

/// Takes the group off another user's pass.
async fn remove_allowlist_case(env: &mut Env, publisher: bool) -> Case {
    let mgroup = env.mgroup().await;
    let other_user = Pubkey::new_unique();
    let pass = env.access_pass(other_user, AccessPassType::Prepaid).await;
    env.allowlist(mgroup, pass, other_user, publisher).await;
    let ix = if publisher {
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip: IP,
                user_payer: other_user,
            },
        )
    } else {
        DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
            RemoveMulticastGroupSubAllowlistArgs {
                client_ip: IP,
                user_payer: other_user,
            },
        )
    };
    Case {
        ix,
        accounts: env.signed(vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(pass, false),
            env.gs_meta(),
        ]),
        watched: vec![pass, mgroup],
        refused: DoubleZeroError::NotAllowed,
    }
}

async fn remove_mgroup_pub_allowlist(env: &mut Env) -> Case {
    remove_allowlist_case(env, true).await
}

async fn remove_mgroup_sub_allowlist(env: &mut Env) -> Case {
    remove_allowlist_case(env, false).await
}

/// Strips the publisher role from a user the foundation member owns.
async fn update_mgroup_roles(env: &mut Env) -> Case {
    let dev = env.network(128).await;
    let mgroup = env.mgroup().await;
    let owner = env.payer.pubkey();
    let pass = env.access_pass(owner, AccessPassType::Prepaid).await;
    env.allowlist(mgroup, pass, owner, true).await;
    let (user, _) = get_user_pda(&env.program_id, &IP, UserType::Multicast);
    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(dev.pk, false),
        AccountMeta::new(mgroup, false),
        AccountMeta::new(pass, false),
        AccountMeta::new(env.gs, false),
    ];
    accounts.extend(env.user_resources(&dev));
    env.exec(
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: IP,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
            ip_proof: None,
            extra_group_count: 0,
        }),
        accounts,
    )
    .await;
    Case {
        ix: DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
            client_ip: IP,
            publisher: false,
            subscriber: false,
            use_onchain_allocation: true,
            extra_group_count: 0,
        }),
        accounts: env.signed(vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(pass, false),
            AccountMeta::new(user, false),
            env.gs_meta(),
            AccountMeta::new(env.resource(ResourceType::MulticastPublisherBlock), false),
        ]),
        watched: vec![user, mgroup],
        refused: DoubleZeroError::NotAllowed,
    }
}

// --- access passes -----------------------------------------------------------------------------

/// The outsider issues itself a pass.
async fn set_access_pass(env: &mut Env) -> Case {
    let outsider = env.outsider.pubkey();
    let (pass, _) = get_accesspass_pda(&env.program_id, &IP, &outsider);
    Case {
        ix: DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: IP,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        accounts: env.signed(vec![
            AccountMeta::new(pass, false),
            env.gs_meta(),
            AccountMeta::new(outsider, false),
        ]),
        watched: vec![pass],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// An instruction on another user's existing pass, with the trailing `[accesspass, globalstate]`
/// layout the access-pass handlers share.
async fn other_pass_case(env: &mut Env, ix: DoubleZeroInstruction) -> Case {
    let pass = env
        .access_pass(Pubkey::new_unique(), AccessPassType::Prepaid)
        .await;
    Case {
        ix,
        accounts: env.signed(vec![AccountMeta::new(pass, false), env.gs_meta()]),
        watched: vec![pass],
        refused: DoubleZeroError::NotAllowed,
    }
}

async fn close_access_pass(env: &mut Env) -> Case {
    other_pass_case(
        env,
        DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
    )
    .await
}

async fn close_prepaid_access_pass(env: &mut Env) -> Case {
    other_pass_case(
        env,
        DoubleZeroInstruction::ClosePrepaidAccessPass(CloseAccessPassArgs {}),
    )
    .await
}

async fn set_access_pass_flags(env: &mut Env) -> Case {
    other_pass_case(
        env,
        DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
            allow_multiple_ip: None,
            dzf_locked: Some(true),
        }),
    )
    .await
}

/// The pass is already `Requested`, so only the result is observable.
async fn check_status_access_pass(env: &mut Env) -> Case {
    other_pass_case(
        env,
        DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
    )
    .await
}

/// Wipes the feed seats off another user's EdgeSeat pass.
async fn set_access_pass_feeds(env: &mut Env) -> Case {
    let other_user = Pubkey::new_unique();
    let pass = env
        .access_pass(other_user, AccessPassType::EdgeSeat(vec![]))
        .await;
    // Seeded: provisioning a real seat needs a Feed account, which is beside the point here.
    env.rewrite_access_pass(pass, |pass| {
        pass.accesspass_type = AccessPassType::EdgeSeat(vec![FeedSeat {
            feed_key: Pubkey::new_unique(),
            max_users: 1,
            max_future_users: 1,
            current_users: 0,
            anniversary_day: 1,
            window_end: 4_000_000_000,
            terminates_at: 4_100_000_000,
        }]);
    })
    .await;
    Case {
        ix: DoubleZeroInstruction::SetAccessPassFeeds(SetAccessPassFeedsArgs {
            client_ip: IP,
            user_payer: other_user,
            feeds: vec![],
        }),
        accounts: env.signed(vec![AccountMeta::new(pass, false), env.gs_meta()]),
        watched: vec![pass],
        refused: DoubleZeroError::NotAllowed,
    }
}

// --- permissions -------------------------------------------------------------------------------

/// The signer grants itself FOUNDATION.
async fn create_permission(env: &mut Env) -> Case {
    let outsider = env.outsider.pubkey();
    let (permission, _) = get_permission_pda(&env.program_id, &outsider);
    Case {
        ix: DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: outsider,
            permissions: permission_flags::FOUNDATION | permission_flags::PERMISSION_ADMIN,
        }),
        accounts: env.signed(vec![AccountMeta::new(permission, false), env.gs_meta()]),
        watched: vec![permission],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// Raises an accomplice's Permission to FOUNDATION.
async fn update_permission(env: &mut Env) -> Case {
    let permission = env
        .permission(Pubkey::new_unique(), permission_flags::USER_ADMIN)
        .await;
    Case {
        ix: DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
            add: permission_flags::FOUNDATION,
            remove: 0,
        }),
        accounts: env.signed(vec![AccountMeta::new(permission, false), env.gs_meta()]),
        watched: vec![permission],
        refused: DoubleZeroError::NotAllowed,
    }
}

// --- links -------------------------------------------------------------------------------------

/// Accepts, for side Z, a link the outsider's key has no contributor stake in.
async fn accept_link(env: &mut Env) -> Case {
    let site = env.site().await;
    let contributor = site.2;
    let side_a = env
        .device(site, "a", [100, 0, 0, 1], "110.1.0.0/24", 0)
        .await;
    let side_z = env.device(site, "z", [11, 0, 0, 1], "11.1.0.0/23", 0).await;
    env.interface(&side_a, contributor, "Ethernet0").await;
    env.interface(&side_z, contributor, "Ethernet1").await;
    let payer = env.payer.insecure_clone();
    let topology = create_unicast_default_topology(
        &mut env.ctx.banks_client,
        env.program_id,
        env.gs,
        env.gc,
        &payer,
    )
    .await;
    let tunnel_block = env.resource(ResourceType::DeviceTunnelBlock);
    let link_ids = env.resource(ResourceType::LinkIds);
    let index = env.next_index().await;
    let (link, _) = get_link_pda(&env.program_id, index);
    env.exec(
        DoubleZeroInstruction::CreateLink(LinkCreateArgs {
            code: "a-z".to_string(),
            link_type: LinkLinkType::DZX,
            bandwidth: 15_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: None,
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(link, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(side_a.pk, false),
            AccountMeta::new(side_z.pk, false),
            AccountMeta::new(env.gs, false),
            AccountMeta::new(topology, false),
            AccountMeta::new(tunnel_block, false),
            AccountMeta::new(link_ids, false),
        ],
    )
    .await;
    Case {
        ix: DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
            side_z_iface_name: "Ethernet1".to_string(),
            use_onchain_allocation: true,
        }),
        accounts: env.signed(vec![
            AccountMeta::new(link, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(side_z.pk, false),
            env.gs_meta(),
            AccountMeta::new(side_a.pk, false),
            AccountMeta::new(tunnel_block, false),
            AccountMeta::new(link_ids, false),
        ]),
        watched: vec![link, side_a.pk, side_z.pk],
        refused: DoubleZeroError::NotAllowed,
    }
}

// --- infrastructure ----------------------------------------------------------------------------

/// The PDA is indexed from the copy, which the instruction also writes back.
async fn create_location(env: &mut Env) -> Case {
    let index = env.next_index().await;
    let (location, _) = get_location_pda(&env.program_id, index);
    Case {
        ix: DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
            code: "evil".to_string(),
            name: "Evil".to_string(),
            country: "us".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }),
        accounts: env.signed(vec![AccountMeta::new(location, false), env.gs_meta()]),
        watched: vec![location],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// A contributor the outsider owns.
async fn create_contributor(env: &mut Env) -> Case {
    let index = env.next_index().await;
    let (contributor, _) = get_contributor_pda(&env.program_id, index);
    let outsider = env.outsider.pubkey();
    Case {
        ix: DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "evil".to_string(),
        }),
        accounts: env.signed(vec![
            AccountMeta::new(contributor, false),
            AccountMeta::new(outsider, false),
            env.gs_meta(),
        ]),
        watched: vec![contributor],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// A multicast group the outsider owns.
async fn create_multicastgroup(env: &mut Env) -> Case {
    let index = env.next_index().await;
    let (mgroup, _) = get_multicastgroup_pda(&env.program_id, index);
    let block = env.resource(ResourceType::MulticastGroupBlock);
    Case {
        ix: DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "evil".to_string(),
            max_bandwidth: 1000,
            owner: env.outsider.pubkey(),
            use_onchain_allocation: true,
            bond_ref: Pubkey::default(),
        }),
        accounts: env.signed(vec![
            AccountMeta::new(mgroup, false),
            env.gs_meta(),
            AccountMeta::new(block, false),
        ]),
        watched: vec![mgroup, block],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// Deletes a group the foundation member owns.
async fn delete_multicastgroup(env: &mut Env) -> Case {
    let mgroup = env.mgroup().await;
    let block = env.resource(ResourceType::MulticastGroupBlock);
    Case {
        ix: DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
            use_onchain_deallocation: true,
        }),
        accounts: env.signed(vec![
            AccountMeta::new(mgroup, false),
            env.gs_meta(),
            AccountMeta::new(block, false),
            AccountMeta::new(env.payer.pubkey(), false),
        ]),
        watched: vec![mgroup, block],
        refused: DoubleZeroError::NotAllowed,
    }
}

/// Makes the outsider an administrator of someone else's tenant.
async fn tenant_add_administrator(env: &mut Env) -> Case {
    let (tenant, _) = get_tenant_pda(&env.program_id, "tenant");
    let vrf_ids = env.resource(ResourceType::VrfIds);
    env.exec(
        DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
            code: "tenant".to_string(),
            administrator: Pubkey::new_unique(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }),
        vec![
            AccountMeta::new(tenant, false),
            AccountMeta::new(env.gs, false),
            AccountMeta::new(vrf_ids, false),
        ],
    )
    .await;
    Case {
        ix: DoubleZeroInstruction::TenantAddAdministrator(TenantAddAdministratorArgs {
            administrator: env.outsider.pubkey(),
        }),
        accounts: env.signed(vec![AccountMeta::new(tenant, false), env.gs_meta()]),
        watched: vec![tenant],
        refused: DoubleZeroError::NotAllowed,
    }
}

// --- global state ------------------------------------------------------------------------------

fn globalstate_case(env: &Env, ix: DoubleZeroInstruction) -> Case {
    Case {
        ix,
        accounts: env.signed(vec![env.gs_meta()]),
        watched: vec![],
        refused: DoubleZeroError::NotAllowed,
    }
}

async fn add_foundation_allowlist(env: &mut Env) -> Case {
    let pubkey = env.outsider.pubkey();
    globalstate_case(
        env,
        DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs { pubkey }),
    )
}

async fn remove_foundation_allowlist(env: &mut Env) -> Case {
    let pubkey = env.payer.pubkey();
    globalstate_case(
        env,
        DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs { pubkey }),
    )
}

async fn add_qa_allowlist(env: &mut Env) -> Case {
    let pubkey = env.outsider.pubkey();
    globalstate_case(
        env,
        DoubleZeroInstruction::AddQaAllowlist(AddQaAllowlistArgs { pubkey }),
    )
}

async fn remove_qa_allowlist(env: &mut Env) -> Case {
    let pubkey = env.payer.pubkey();
    globalstate_case(
        env,
        DoubleZeroInstruction::RemoveQaAllowlist(RemoveQaAllowlistArgs { pubkey }),
    )
}

async fn set_authority(env: &mut Env) -> Case {
    let outsider = env.outsider.pubkey();
    globalstate_case(
        env,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            sentinel_authority_pk: Some(outsider),
            ..SetAuthorityArgs::default()
        }),
    )
}

async fn set_feature_flags(env: &mut Env) -> Case {
    globalstate_case(
        env,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
    )
}

/// One test per case and copy owner, named `<case>::<owner>_owned_copy`.
macro_rules! cases {
    ($($case:ident => $expect:expr),* $(,)?) => {$(
        mod $case {
            use super::*;

            #[tokio::test]
            async fn program_owned_copy() {
                let mut env = Env::new().await;
                let case = super::$case(&mut env).await;
                env.check(case, Owner::Program, $expect).await;
            }

            #[tokio::test]
            async fn loader_owned_copy() {
                let mut env = Env::new().await;
                let case = super::$case(&mut env).await;
                env.check(case, Owner::Loader, $expect).await;
            }
        }
    )*};
}

cases! {
    create_user => Expect::Load,
    create_subscribe_user => Expect::Load,
    add_mgroup_pub_allowlist => Expect::Load,
    add_mgroup_sub_allowlist => Expect::Load,
    remove_mgroup_pub_allowlist => Expect::Load,
    remove_mgroup_sub_allowlist => Expect::Load,
    accept_link => Expect::Load,
    delete_user => Expect::OwnerAsserted,
    delete_prepaid_user => Expect::OwnerAsserted,
    set_access_pass => Expect::OwnerAsserted,
    close_access_pass => Expect::OwnerAsserted,
    close_prepaid_access_pass => Expect::OwnerAsserted,
    set_access_pass_flags => Expect::OwnerAsserted,
    set_access_pass_feeds => Expect::OwnerAsserted,
    check_status_access_pass => Expect::OwnerAsserted,
    create_permission => Expect::OwnerAsserted,
    update_permission => Expect::OwnerAsserted,
    create_location => Expect::OwnerAsserted,
    create_contributor => Expect::OwnerAsserted,
    create_multicastgroup => Expect::OwnerAsserted,
    delete_multicastgroup => Expect::OwnerAsserted,
    tenant_add_administrator => Expect::OwnerAsserted,
    update_mgroup_roles => Expect::Rejected,
    add_foundation_allowlist => Expect::Rejected,
    remove_foundation_allowlist => Expect::Rejected,
    add_qa_allowlist => Expect::Rejected,
    remove_qa_allowlist => Expect::Rejected,
    set_authority => Expect::Rejected,
    set_feature_flags => Expect::Rejected,
}
