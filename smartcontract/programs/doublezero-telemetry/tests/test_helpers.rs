#![allow(dead_code)]
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_contributor_pda, get_device_pda, get_exchange_pda, get_globalconfig_pda,
        get_globalstate_pda, get_link_pda, get_location_pda, get_program_config_pda,
        get_resource_extension_pda,
    },
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{
            activate::DeviceActivateArgs,
            create::DeviceCreateArgs,
            interface::{create::DeviceInterfaceCreateArgs, unlink::DeviceInterfaceUnlinkArgs},
            sethealth::DeviceSetHealthArgs,
            update::DeviceUpdateArgs,
        },
        exchange::{create::ExchangeCreateArgs, suspend::ExchangeSuspendArgs},
        globalconfig::set::SetGlobalConfigArgs,
        link::{
            activate::LinkActivateArgs, create::LinkCreateArgs, sethealth::LinkSetHealthArgs,
            update::LinkUpdateArgs,
        },
        location::{create::LocationCreateArgs, suspend::LocationSuspendArgs},
    },
    resource::ResourceType,
    state::{
        device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceType},
        exchange::Exchange,
        globalstate::GlobalState,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType},
        location::Location,
    },
};
use doublezero_telemetry::{
    error::TelemetryError,
    instructions::{TelemetryInstruction, INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX},
    pda::{derive_device_latency_samples_pda, derive_internet_latency_samples_pda},
    processors::telemetry::{
        initialize_device_latency_samples::InitializeDeviceLatencySamplesArgs,
        initialize_internet_latency_samples::InitializeInternetLatencySamplesArgs,
        write_device_latency_samples::WriteDeviceLatencySamplesArgs,
        write_internet_latency_samples::WriteInternetLatencySamplesArgs,
    },
    serviceability_program_id,
};

#[cfg(not(feature = "no-entrypoint"))]
use doublezero_telemetry::entrypoint::process_instruction as telemetry_process_instruction;
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentLevel,
    hash::Hash,
    instruction::{AccountMeta, Instruction, InstructionError},
    message::{v0::Message, VersionedMessage},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    system_program,
    transaction::{Transaction, TransactionError, VersionedTransaction},
};
use std::sync::{Arc, Mutex};

#[ctor::ctor]
fn init_logger() {
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(|| {
        let mut builder = env_logger::builder();

        // If PROGRAM_LOG is set, show the Solana program logs.
        if std::env::var_os("PROGRAM_LOG").is_some() {
            builder.filter_level(log::LevelFilter::Error);
            builder.filter(
                Some("solana_runtime::message_processor::stable_log"),
                log::LevelFilter::Debug,
            );
        }

        let _ = builder.try_init();
    });
}

pub trait LocationCreateArgsExt {
    fn default() -> LocationCreateArgs;
}

impl LocationCreateArgsExt for LocationCreateArgs {
    fn default() -> LocationCreateArgs {
        LocationCreateArgs {
            code: "".to_string(),
            name: "".to_string(),
            country: "".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }
    }
}

pub trait ExchangeCreateArgsExt {
    fn default() -> ExchangeCreateArgs;
}

impl ExchangeCreateArgsExt for ExchangeCreateArgs {
    fn default() -> ExchangeCreateArgs {
        ExchangeCreateArgs {
            code: "".to_string(),
            name: "".to_string(),
            lat: 0.0,
            lng: 0.0,
            reserved: 0,
        }
    }
}

pub trait DeviceCreateArgsExt {
    fn default() -> DeviceCreateArgs;
}

impl DeviceCreateArgsExt for DeviceCreateArgs {
    fn default() -> DeviceCreateArgs {
        DeviceCreateArgs {
            code: "".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: "100.0.0.1".parse().unwrap(),
            dz_prefixes: vec!["110.0.0.0/24".parse().unwrap()].into(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: String::default(),
            desired_status: Some(DeviceDesiredStatus::Activated),
        }
    }
}

pub trait LinkCreateArgsExt {
    fn default() -> LinkCreateArgs;
}

impl LinkCreateArgsExt for LinkCreateArgs {
    fn default() -> LinkCreateArgs {
        LinkCreateArgs {
            code: "".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 4500,
            delay_ns: 10000,
            jitter_ns: 10000,
            side_a_iface_name: String::default(),
            side_z_iface_name: Some(String::default()),
            desired_status: Some(LinkDesiredStatus::Activated),
        }
    }
}

pub struct LedgerContext {
    pub banks_client: BanksClient,
    pub payer: Keypair,
    pub recent_blockhash: Hash,
}

pub struct LedgerHelper {
    pub context: Arc<Mutex<LedgerContext>>,
    pub serviceability: ServiceabilityProgramHelper,
    pub telemetry: TelemetryProgramHelper,
}

impl LedgerHelper {
    pub async fn new() -> Result<Self, BanksClientError> {
        Self::new_with_preloaded_accounts(vec![]).await
    }

    pub async fn new_with_preloaded_accounts(
        preloaded_accounts: Vec<(Pubkey, Account)>,
    ) -> Result<Self, BanksClientError> {
        let (mut program_test, telemetry_program_id, serviceability_program_id) =
            setup_test_programs();

        for (pk, account) in preloaded_accounts {
            program_test.add_account(pk, account);
        }

        let (banks_client, payer, recent_blockhash) = program_test.start().await;

        let context = Arc::new(Mutex::new(LedgerContext {
            banks_client,
            payer,
            recent_blockhash,
        }));

        let serviceability =
            ServiceabilityProgramHelper::new(context.clone(), serviceability_program_id).await?;

        let telemetry = TelemetryProgramHelper::new(context.clone(), telemetry_program_id).await?;

        Ok(Self {
            context,
            serviceability,
            telemetry,
        })
    }

    pub async fn get_account(
        &mut self,
        pubkey: Pubkey,
    ) -> Result<Option<Account>, BanksClientError> {
        let banks_client = { self.context.lock().unwrap().banks_client.clone() };
        banks_client.get_account(pubkey).await
    }

    pub async fn refresh_blockhash(&mut self) -> Result<(), BanksClientError> {
        let banks_client = { self.context.lock().unwrap().banks_client.clone() };
        let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
        self.context.lock().unwrap().recent_blockhash = recent_blockhash;
        Ok(())
    }

    pub async fn wait_for_new_blockhash(&mut self) -> Result<(), BanksClientError> {
        let banks_client = { self.context.lock().unwrap().banks_client.clone() };
        let current_blockhash = self.context.lock().unwrap().recent_blockhash;

        let mut new_blockhash = current_blockhash;
        while new_blockhash == current_blockhash {
            new_blockhash = banks_client.get_latest_blockhash().await?;
            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        }

        {
            let mut context = self.context.lock().unwrap();
            context.recent_blockhash = new_blockhash;
        }
        Ok(())
    }

    pub async fn fund_account(
        &mut self,
        recipient: &Pubkey,
        lamports: u64,
    ) -> Result<(), BanksClientError> {
        let (banks_client, payer, recent_blockhash) = {
            let context = self.context.lock().unwrap();
            (
                context.banks_client.clone(),
                context.payer.insecure_clone(),
                context.recent_blockhash,
            )
        };
        let transfer_instruction =
            solana_sdk::system_instruction::transfer(&payer.pubkey(), recipient, lamports);
        let mut transaction =
            Transaction::new_with_payer(&[transfer_instruction], Some(&payer.pubkey()));
        transaction.sign(&[&payer], recent_blockhash);
        banks_client.process_transaction(transaction).await
    }

    pub async fn create_account_raw(
        &mut self,
        funder: &Keypair,
        new_account: &Pubkey,
        lamports: u64,
        space: u64,
        owner: &Pubkey,
    ) -> Result<(), BanksClientError> {
        let ix = solana_sdk::system_instruction::create_account(
            &funder.pubkey(),
            new_account,
            lamports,
            space,
            owner,
        );

        let banks_client = {
            let ctx = self.context.lock().unwrap();
            ctx.banks_client.clone()
        };
        let blockhash = banks_client.get_latest_blockhash().await?;
        let tx = solana_sdk::transaction::Transaction::new_signed_with_payer(
            &[ix],
            Some(&funder.pubkey()),
            &[funder],
            blockhash,
        );

        banks_client.process_transaction(tx).await
    }

    pub async fn seed_with_two_linked_devices(
        &mut self,
        contributor_pk: Pubkey,
    ) -> Result<(Keypair, Pubkey, Pubkey, Pubkey), BanksClientError> {
        // Create alocation.
        let location_pk = self
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Test Location".to_string(),
                country: "US".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await?;

        // Create an exchange.
        let exchange_pk = self
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange".to_string(),
                reserved: 0,
                ..ExchangeCreateArgs::default()
            })
            .await?;

        // Create and fund origin device agent account.
        let origin_device_agent = Keypair::new();
        let origin_device_agent_pk = origin_device_agent.pubkey();
        self.fund_account(&origin_device_agent_pk, 10_000_000_000)
            .await?;

        // Create and activate origin device.
        let origin_device_pk = self
            .serviceability
            .create_and_activate_device(
                DeviceCreateArgs {
                    code: "origin_device".to_string(),
                    device_type: DeviceType::Hybrid,
                    public_ip: [100, 0, 0, 1].into(),
                    dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                    metrics_publisher_pk: origin_device_agent_pk,
                    ..DeviceCreateArgs::default()
                },
                contributor_pk,
                location_pk,
                exchange_pk,
            )
            .await?;

        self.serviceability
            .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
            .await?;

        // Create and activate target device.
        let target_device_pk = self
            .serviceability
            .create_and_activate_device(
                DeviceCreateArgs {
                    code: "target_device".to_string(),
                    device_type: DeviceType::Hybrid,
                    public_ip: [100, 0, 0, 2].into(),
                    dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                    metrics_publisher_pk: Pubkey::new_unique(),
                    ..DeviceCreateArgs::default()
                },
                contributor_pk,
                location_pk,
                exchange_pk,
            )
            .await?;

        self.serviceability
            .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
            .await?;

        // Create and activate link.
        let link_pk = self
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".to_string(),
                    link_type: LinkLinkType::WAN,
                    bandwidth: 10_000_000_000,
                    mtu: 4500,
                    delay_ns: 1000000,
                    jitter_ns: 100000,
                    side_a_iface_name: "Ethernet0".to_string(),
                    side_z_iface_name: Some("Ethernet1".to_string()),
                    desired_status: Some(LinkDesiredStatus::Activated),
                },
                contributor_pk,
                origin_device_pk,
                target_device_pk,
                1,
                "10.1.1.0/30".parse().unwrap(),
            )
            .await?;

        Ok((
            origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
        ))
    }

    pub async fn seed_with_two_exchanges(
        &mut self,
    ) -> Result<(Keypair, Pubkey, Pubkey), BanksClientError> {
        // create exchanges
        let exchange1_pk = self
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange1".to_string(),
                reserved: 0,
                ..ExchangeCreateArgs::default()
            })
            .await?;

        let exchange2_pk = self
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX2".to_string(),
                name: "Test Exchange2".to_string(),
                reserved: 0,
                ..ExchangeCreateArgs::default()
            })
            .await?;

        // Create and fund the internet samples agent oracle
        let oracle_agent = Keypair::new();
        let oracle_agent_pk = oracle_agent.pubkey();
        self.fund_account(&oracle_agent_pk, 10_000_000_000).await?;

        Ok((oracle_agent, exchange1_pk, exchange2_pk))
    }
}

pub struct TelemetryProgramHelper {
    context: Arc<Mutex<LedgerContext>>,
    pub program_id: Pubkey,
}

impl TelemetryProgramHelper {
    pub async fn new(
        context: Arc<Mutex<LedgerContext>>,
        program_id: Pubkey,
    ) -> Result<Self, BanksClientError> {
        Ok(Self {
            context,
            program_id,
        })
    }

    pub async fn initialize_device_latency_samples(
        &mut self,
        agent: &Keypair,
        origin_device_pk: Pubkey,
        target_device_pk: Pubkey,
        link_pk: Pubkey,
        epoch: u64,
        sampling_interval_microseconds: u64,
    ) -> Result<Pubkey, BanksClientError> {
        let (pda, _) = derive_device_latency_samples_pda(
            &self.program_id,
            &origin_device_pk,
            &target_device_pk,
            &link_pk,
            epoch,
        );

        self.initialize_device_latency_samples_with_pda(
            agent,
            pda,
            origin_device_pk,
            target_device_pk,
            link_pk,
            epoch,
            sampling_interval_microseconds,
        )
        .await?;

        Ok(pda)
    }

    pub async fn write_device_latency_samples(
        &mut self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        samples: Vec<u32>,
        start_timestamp_microseconds: u64,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            TelemetryInstruction::WriteDeviceLatencySamples(WriteDeviceLatencySamplesArgs {
                start_timestamp_microseconds,
                samples,
            }),
            &[agent],
            vec![
                AccountMeta::new(latency_samples_pda, false),
                AccountMeta::new(agent.pubkey(), true),
                AccountMeta::new_readonly(system_program::id(), false),
            ],
        )
        .await
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn initialize_device_latency_samples_with_pda(
        &mut self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        origin_device_pk: Pubkey,
        target_device_pk: Pubkey,
        link_pk: Pubkey,
        epoch: u64,
        interval_us: u64,
    ) -> Result<Pubkey, BanksClientError> {
        let args = InitializeDeviceLatencySamplesArgs {
            epoch,
            sampling_interval_microseconds: interval_us,
        };

        self.execute_transaction(
            TelemetryInstruction::InitializeDeviceLatencySamples(args),
            &[agent],
            vec![
                AccountMeta::new(latency_samples_pda, false),
                AccountMeta::new_readonly(agent.pubkey(), true),
                AccountMeta::new_readonly(origin_device_pk, false),
                AccountMeta::new_readonly(target_device_pk, false),
                AccountMeta::new_readonly(link_pk, false),
                AccountMeta::new_readonly(solana_program::system_program::id(), false),
            ],
        )
        .await?;

        Ok(latency_samples_pda)
    }

    pub async fn write_device_latency_samples_with_pda(
        &self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        samples: Vec<u32>,
        timestamp: u64,
    ) -> Result<(), BanksClientError> {
        let args = WriteDeviceLatencySamplesArgs {
            start_timestamp_microseconds: timestamp,
            samples,
        };

        let ix = TelemetryInstruction::WriteDeviceLatencySamples(args)
            .pack()
            .expect("failed to pack");

        let accounts = vec![
            AccountMeta::new(latency_samples_pda, false),
            AccountMeta::new_readonly(agent.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ];

        let instruction = solana_sdk::instruction::Instruction {
            program_id: self.program_id,
            accounts,
            data: ix,
        };

        let (banks_client, payer, recent_blockhash) = {
            let ctx = self.context.lock().unwrap();
            (
                ctx.banks_client.clone(),
                ctx.payer.insecure_clone(),
                ctx.recent_blockhash,
            )
        };

        let tx = Transaction::new_signed_with_payer(
            &[instruction],
            Some(&payer.pubkey()),
            &[&payer, agent],
            recent_blockhash,
        );

        banks_client.process_transaction(tx).await
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn initialize_internet_latency_samples(
        &mut self,
        agent: &Keypair,
        data_provider_name: String,
        origin_location_pk: Pubkey,
        target_location_pk: Pubkey,
        epoch: u64,
        sampling_interval_micros: u64,
    ) -> Result<Pubkey, BanksClientError> {
        let (pda, _) = derive_internet_latency_samples_pda(
            &self.program_id,
            &agent.pubkey(),
            &data_provider_name,
            &origin_location_pk,
            &target_location_pk,
            epoch,
        );

        self.initialize_internet_latency_samples_with_pda(
            agent,
            pda,
            data_provider_name,
            origin_location_pk,
            target_location_pk,
            epoch,
            sampling_interval_micros,
        )
        .await?;

        Ok(pda)
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn initialize_internet_latency_samples_with_pda(
        &mut self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        data_provider_name: String,
        origin_location_pk: Pubkey,
        target_location_pk: Pubkey,
        epoch: u64,
        interval_micros: u64,
    ) -> Result<Pubkey, BanksClientError> {
        let args = InitializeInternetLatencySamplesArgs {
            data_provider_name,
            epoch,
            sampling_interval_microseconds: interval_micros,
        };

        self.execute_transaction(
            TelemetryInstruction::InitializeInternetLatencySamples(args),
            &[agent],
            vec![
                AccountMeta::new(latency_samples_pda, false),
                AccountMeta::new(agent.pubkey(), true),
                AccountMeta::new(origin_location_pk, false),
                AccountMeta::new(target_location_pk, false),
                AccountMeta::new(solana_program::system_program::id(), false),
            ],
        )
        .await?;

        Ok(latency_samples_pda)
    }

    pub async fn write_internet_latency_samples(
        &mut self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        samples: Vec<u32>,
        start_timestamp_microseconds: u64,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            TelemetryInstruction::WriteInternetLatencySamples(WriteInternetLatencySamplesArgs {
                start_timestamp_microseconds,
                samples,
            }),
            &[agent],
            vec![
                AccountMeta::new(latency_samples_pda, false),
                AccountMeta::new(agent.pubkey(), true),
                AccountMeta::new(solana_program::system_program::id(), false),
            ],
        )
        .await
    }

    pub async fn write_internet_latency_samples_with_pda(
        &self,
        agent: &Keypair,
        latency_samples_pda: Pubkey,
        samples: Vec<u32>,
        timestamp: u64,
    ) -> Result<(), BanksClientError> {
        let args = WriteInternetLatencySamplesArgs {
            start_timestamp_microseconds: timestamp,
            samples,
        };

        let ix = TelemetryInstruction::WriteInternetLatencySamples(args)
            .pack()
            .expect("failed to pack");

        let accounts = vec![
            AccountMeta::new(latency_samples_pda, false),
            AccountMeta::new(agent.pubkey(), true),
            AccountMeta::new(solana_program::system_program::id(), false),
        ];

        let instruction = Instruction {
            program_id: self.program_id,
            accounts,
            data: ix,
        };

        let (banks_client, payer, recent_blockhash) = {
            let ctx = self.context.lock().unwrap();
            (
                ctx.banks_client.clone(),
                ctx.payer.insecure_clone(),
                ctx.recent_blockhash,
            )
        };

        let tx = Transaction::new_signed_with_payer(
            &[instruction],
            Some(&payer.pubkey()),
            &[&payer, agent],
            recent_blockhash,
        );

        banks_client.process_transaction(tx).await
    }

    pub async fn execute_transaction(
        &mut self,
        instruction: TelemetryInstruction,
        signers: &[&Keypair],
        accounts: Vec<AccountMeta>,
    ) -> Result<(), BanksClientError> {
        let (mut banks_client, recent_blockhash) = {
            let context = self.context.lock().unwrap();
            (context.banks_client.clone(), context.recent_blockhash)
        };
        execute_transaction(
            &mut banks_client,
            signers,
            recent_blockhash,
            self.program_id,
            instruction,
            accounts,
        )
        .await
    }
}

pub struct ServiceabilityProgramHelper {
    context: Arc<Mutex<LedgerContext>>,
    pub program_id: Pubkey,

    pub global_state_pubkey: Pubkey,
    pub global_config_pubkey: Pubkey,
}

impl ServiceabilityProgramHelper {
    pub async fn new(
        context: Arc<Mutex<LedgerContext>>,
        program_id: Pubkey,
    ) -> Result<Self, BanksClientError> {
        let (global_state_pubkey, global_config_pubkey) = {
            let (mut banks_client, payer, recent_blockhash) = {
                let context = context.lock().unwrap();
                (
                    context.banks_client.clone(),
                    context.payer.insecure_clone(),
                    context.recent_blockhash,
                )
            };

            let (program_config_pubkey, _) = get_program_config_pda(&program_id);

            let (global_state_pubkey, _) = get_globalstate_pda(&program_id);
            execute_serviceability_instruction(
                &mut banks_client,
                &payer,
                recent_blockhash,
                program_id,
                DoubleZeroInstruction::InitGlobalState(),
                vec![
                    AccountMeta::new(program_config_pubkey, false),
                    AccountMeta::new(global_state_pubkey, false),
                ],
            )
            .await?;

            let (global_config_pubkey, _) = get_globalconfig_pda(&program_id);
            let (device_tunnel_block_pda, _, _) =
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
            let (user_tunnel_block_pda, _, _) =
                get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
            let (multicastgroup_block_pda, _, _) =
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
            let (link_ids_pda, _, _) =
                get_resource_extension_pda(&program_id, ResourceType::LinkIds);
            let (segment_routing_ids_pda, _, _) =
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
            let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
            execute_serviceability_instruction(
                &mut banks_client,
                &payer,
                recent_blockhash,
                program_id,
                DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                    local_asn: 65000,
                    remote_asn: 65001,
                    device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                    user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                    multicastgroup_block: "224.0.0.0/24".parse().unwrap(),
                    next_bgp_community: None,
                }),
                vec![
                    AccountMeta::new(global_config_pubkey, false),
                    AccountMeta::new(global_state_pubkey, false),
                    AccountMeta::new(device_tunnel_block_pda, false),
                    AccountMeta::new(user_tunnel_block_pda, false),
                    AccountMeta::new(multicastgroup_block_pda, false),
                    AccountMeta::new(link_ids_pda, false),
                    AccountMeta::new(segment_routing_ids_pda, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ],
            )
            .await?;

            (global_state_pubkey, global_config_pubkey)
        };

        Ok(Self {
            context,
            program_id,

            global_state_pubkey,
            global_config_pubkey,
        })
    }

    pub async fn get_next_global_state_index(&mut self) -> Result<u128, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let account = banks_client
            .get_account(self.global_state_pubkey)
            .await
            .map_err(|e| {
                println!("Error getting global state account: {e:?}");
                e
            })?
            .ok_or(BanksClientError::ClientError(
                "Global state account not found",
            ))?;
        let global_state = GlobalState::try_from(&account.data[..]).unwrap();
        Ok(global_state.account_index + 1)
    }

    pub async fn create_location(
        &mut self,
        location: LocationCreateArgs,
    ) -> Result<Pubkey, BanksClientError> {
        let index = self.get_next_global_state_index().await?;
        let (location_pubkey, _) = get_location_pda(&self.program_id, index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                code: location.code,
                name: location.name,
                country: location.country,
                lat: location.lat,
                lng: location.lng,
                loc_id: location.loc_id,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(location_pubkey)
    }

    pub async fn create_exchange(
        &mut self,
        exchange: ExchangeCreateArgs,
    ) -> Result<Pubkey, BanksClientError> {
        let index = self.get_next_global_state_index().await?;
        let (exchange_pubkey, _) = get_exchange_pda(&self.program_id, index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                code: exchange.code,
                name: exchange.name,
                lat: exchange.lat,
                lng: exchange.lng,
                reserved: 0, // BGP community is auto-assigned
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(self.global_config_pubkey, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(exchange_pubkey)
    }

    pub async fn create_contributor(
        &mut self,
        code: String,
        owner: Pubkey,
    ) -> Result<Pubkey, BanksClientError> {
        let index = self.get_next_global_state_index().await.unwrap();
        let (contributor_pk, _) = get_contributor_pda(&self.program_id, index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateContributor(ContributorCreateArgs { code }),
            vec![
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(contributor_pk)
    }

    pub async fn create_device(
        &mut self,
        device: DeviceCreateArgs,
        contributor_pk: Pubkey,
        location_pk: Pubkey,
        exchange_pk: Pubkey,
    ) -> Result<Pubkey, BanksClientError> {
        let index = self.get_next_global_state_index().await?;
        let (device_pk, _) = get_device_pda(&self.program_id, index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                code: device.code,
                device_type: device.device_type,
                public_ip: device.public_ip,
                dz_prefixes: device.dz_prefixes,
                metrics_publisher_pk: device.metrics_publisher_pk,
                mgmt_vrf: device.mgmt_vrf,
                desired_status: Some(DeviceDesiredStatus::Activated),
            }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(location_pk, false),
                AccountMeta::new(exchange_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(device_pk)
    }

    pub async fn activate_device(
        &mut self,
        device_pk: Pubkey,
        contributor_pk: Pubkey,
        resource_count: usize,
    ) -> Result<(), BanksClientError> {
        let (globalconfig_pda, _) = get_globalconfig_pda(&self.program_id);
        let mut resources = vec![];
        resources.push(AccountMeta::new(
            get_resource_extension_pda(&self.program_id, ResourceType::TunnelIds(device_pk, 0)).0,
            false,
        ));
        for i in 1..resource_count {
            resources.push(AccountMeta::new(
                get_resource_extension_pda(
                    &self.program_id,
                    ResourceType::DzPrefixBlock(device_pk, i - 1),
                )
                .0,
                false,
            ));
        }
        self.execute_transaction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count }),
            [
                vec![
                    AccountMeta::new(device_pk, false),
                    AccountMeta::new(self.global_state_pubkey, false),
                    AccountMeta::new(globalconfig_pda, false),
                ],
                resources,
            ]
            .concat(),
        )
        .await?;

        self.execute_transaction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                desired_status: Some(DeviceDesiredStatus::Activated),
                ..Default::default()
            }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        self.execute_transaction(
            DoubleZeroInstruction::SetDeviceHealth(DeviceSetHealthArgs {
                health: DeviceHealth::ReadyForUsers,
            }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn get_device(&mut self, pubkey: Pubkey) -> Result<Device, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let device = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Device::try_from(&device.data[..]).unwrap())
    }

    pub async fn softdrained_device(
        &mut self,
        contributor_pk: Pubkey,
        pubkey: Pubkey,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                desired_status: Some(DeviceDesiredStatus::Drained),
                ..Default::default()
            }),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn create_interface(
        &mut self,
        device_pk: Pubkey,
        contributor_pk: Pubkey,
        name: String,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
                name: name.clone(),
                interface_dia: InterfaceDIA::None,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                bandwidth: 0,
                cir: 0,
                ip_net: None,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;
        self.execute_transaction(
            DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs { name }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn get_location(&mut self, pubkey: Pubkey) -> Result<Location, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let location = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Location::try_from(&location.data[..]).unwrap())
    }

    pub async fn suspend_location(&mut self, pubkey: Pubkey) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {}),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn get_exchange(&mut self, pubkey: Pubkey) -> Result<Exchange, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let exchange = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Exchange::try_from(&exchange.data[..]).unwrap())
    }

    pub async fn suspend_exchange(&mut self, pubkey: Pubkey) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs {}),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn create_and_activate_device(
        &mut self,
        device: DeviceCreateArgs,
        contributor_pk: Pubkey,
        location_pk: Pubkey,
        exchange_pk: Pubkey,
    ) -> Result<Pubkey, BanksClientError> {
        let resource_count = 1 + device.dz_prefixes.len();
        let device_pk = self
            .create_device(device, contributor_pk, location_pk, exchange_pk)
            .await?;
        self.activate_device(device_pk, contributor_pk, resource_count)
            .await?;
        Ok(device_pk)
    }

    pub async fn create_link(
        &mut self,
        link: LinkCreateArgs,
        contributor_pk: Pubkey,
        side_a_pk: Pubkey,
        side_z_pk: Pubkey,
    ) -> Result<Pubkey, BanksClientError> {
        let index = self.get_next_global_state_index().await?;
        let (link_pk, _) = get_link_pda(&self.program_id, index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                code: link.code,
                link_type: link.link_type,
                bandwidth: link.bandwidth,
                mtu: link.mtu,
                delay_ns: link.delay_ns,
                jitter_ns: link.jitter_ns,
                side_a_iface_name: link.side_a_iface_name,
                side_z_iface_name: link.side_z_iface_name,
                desired_status: Some(LinkDesiredStatus::Activated),
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(side_a_pk, false),
                AccountMeta::new(side_z_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(link_pk)
    }

    pub async fn activate_link(
        &mut self,
        link_pk: Pubkey,
        contributor_pk: Pubkey,
        side_a_pk: Pubkey,
        side_z_pk: Pubkey,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id,
                tunnel_net,
                use_onchain_allocation: false,
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new(side_a_pk, false),
                AccountMeta::new(side_z_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        self.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                desired_status: Some(LinkDesiredStatus::Activated),
                ..Default::default()
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        self.execute_transaction(
            DoubleZeroInstruction::SetLinkHealth(LinkSetHealthArgs {
                health: LinkHealth::ReadyForService,
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn get_link(&mut self, pubkey: Pubkey) -> Result<Link, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let link = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Link::try_from(&link.data[..]).unwrap())
    }

    pub async fn soft_drain_link(
        &mut self,
        contributor_pk: Pubkey,
        pubkey: Pubkey,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                desired_status: Some(LinkDesiredStatus::SoftDrained),
                ..Default::default()
            }),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn hard_drain_link(
        &mut self,
        contributor_pk: Pubkey,
        pubkey: Pubkey,
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                desired_status: Some(LinkDesiredStatus::HardDrained),
                ..Default::default()
            }),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(contributor_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    pub async fn create_and_activate_link(
        &mut self,
        link: LinkCreateArgs,
        contributor_pk: Pubkey,
        side_a_pk: Pubkey,
        side_z_pk: Pubkey,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
    ) -> Result<Pubkey, BanksClientError> {
        let link_pk = self
            .create_link(link, contributor_pk, side_a_pk, side_z_pk)
            .await?;
        self.activate_link(
            link_pk,
            contributor_pk,
            side_a_pk,
            side_z_pk,
            tunnel_id,
            tunnel_net,
        )
        .await?;
        Ok(link_pk)
    }

    pub async fn execute_transaction(
        &mut self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> Result<(), BanksClientError> {
        let (mut banks_client, payer) = {
            let context = self.context.lock().unwrap();
            (context.banks_client.clone(), context.payer.insecure_clone())
        };

        let latest_blockhash = banks_client.get_latest_blockhash().await?;

        execute_serviceability_instruction(
            &mut banks_client,
            &payer,
            latest_blockhash,
            self.program_id,
            instruction,
            accounts,
        )
        .await
    }
}

pub async fn get_account_data(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<Account> {
    banks_client.get_account(pubkey).await.unwrap()
}

pub async fn fund_account(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    recipient: &Pubkey,
    lamports: u64,
    recent_blockhash: solana_sdk::hash::Hash,
) -> Result<(), BanksClientError> {
    let transfer_instruction =
        solana_sdk::system_instruction::transfer(&payer.pubkey(), recipient, lamports);
    let mut transaction =
        Transaction::new_with_payer(&[transfer_instruction], Some(&payer.pubkey()));
    transaction.sign(&[payer], recent_blockhash);
    banks_client.process_transaction(transaction).await
}

// Execute telemetry transaction with specific signers
pub async fn execute_transaction(
    banks_client: &mut BanksClient,
    signers: &[&Keypair],
    recent_blockhash: solana_sdk::hash::Hash,
    program_id: Pubkey,
    instruction: TelemetryInstruction,
    accounts: Vec<AccountMeta>,
) -> Result<(), BanksClientError> {
    let instruction_data = instruction
        .pack()
        .map_err(|_| BanksClientError::ClientError("Failed to pack instruction"))?;

    let payer = signers[0]; // First signer is always the payer

    let instruction = Instruction {
        program_id,
        accounts,
        data: instruction_data,
    };

    let message =
        Message::try_compile(&payer.pubkey(), &[instruction], &[], recent_blockhash).unwrap();

    let transaction =
        VersionedTransaction::try_new(VersionedMessage::V0(message), signers).unwrap();

    banks_client
        .process_transaction_with_commitment(transaction, CommitmentLevel::Processed)
        .await
        .map_err(|e| {
            println!("Transaction failed: {e:?}");
            e
        })?;
    Ok(())
}

// Helper to execute serviceability instructions for setting up test data
pub async fn execute_serviceability_instruction(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    recent_blockhash: solana_sdk::hash::Hash,
    program_id: Pubkey,
    instruction: doublezero_serviceability::instructions::DoubleZeroInstruction,
    mut accounts: Vec<AccountMeta>,
) -> Result<(), BanksClientError> {
    // Automatically append payer and system_program
    accounts.push(AccountMeta::new(payer.pubkey(), true));
    accounts.push(AccountMeta::new_readonly(system_program::id(), false));

    let instruction_data = borsh::to_vec(&instruction).unwrap();

    let mut transaction = Transaction::new_with_payer(
        &[Instruction {
            program_id,
            accounts,
            data: instruction_data,
        }],
        Some(&payer.pubkey()),
    );
    transaction.sign(&[payer], recent_blockhash);
    banks_client.process_transaction(transaction).await
}

pub fn setup_test_programs() -> (ProgramTest, Pubkey, Pubkey) {
    let mut program_test = ProgramTest::default();
    program_test.set_compute_max_units(1_000_000);

    // Add telemetry program
    let telemetry_program_id = Pubkey::new_unique();
    #[cfg(not(feature = "no-entrypoint"))]
    program_test.add_program(
        "doublezero_telemetry",
        telemetry_program_id,
        processor!(telemetry_process_instruction),
    );
    #[cfg(feature = "no-entrypoint")]
    program_test.add_program("doublezero_telemetry", telemetry_program_id, None);

    // Add serviceability program with its actual processor
    let serviceability_program_id = serviceability_program_id();
    program_test.add_program("doublezero_serviceability", serviceability_program_id, None);

    (
        program_test,
        telemetry_program_id,
        serviceability_program_id,
    )
}

/// Helper function to assert that a result contains a specific telemetry error
pub fn assert_telemetry_error<T>(
    result: Result<T, BanksClientError>,
    expected_error: TelemetryError,
) {
    match result {
        Ok(_) => panic!("Expected error {expected_error:?}, but got Ok"),
        Err(BanksClientError::TransactionError(
            solana_sdk::transaction::TransactionError::InstructionError(
                INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX,
                solana_sdk::instruction::InstructionError::Custom(error_code),
            ),
        )) => {
            assert_eq!(
                error_code, expected_error as u32,
                "Expected error {:?} ({}), got error code {}",
                expected_error, expected_error as u32, error_code
            );
        }
        Err(other) => panic!(
            "Expected telemetry error {:?} ({}), got: {:?}",
            expected_error, expected_error as u32, other
        ),
    }
}

/// Helper function to assert that a result contains a specific banks client error
pub fn assert_banksclient_error<T>(
    result: Result<T, BanksClientError>,
    expected_error: InstructionError,
) {
    match result {
        Ok(_) => panic!("Expected error {expected_error:?}, but got Ok"),
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(_, actual))) => {
            assert_eq!(
                actual, expected_error,
                "Expected error {expected_error:?}, but got {actual:?}"
            );
        }
        Err(other) => panic!("Expected InstructionError {expected_error:?}, but got {other:?}"),
    }
}
