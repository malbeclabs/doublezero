use std::sync::{Arc, Mutex};

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_device_pda, get_exchange_pda, get_globalconfig_pda, get_globalstate_pda, get_link_pda,
        get_location_pda, get_program_config_pda,
    },
    processors::{
        device::{
            activate::DeviceActivateArgs, create::DeviceCreateArgs, suspend::DeviceSuspendArgs,
        },
        exchange::create::ExchangeCreateArgs,
        globalconfig::set::SetGlobalConfigArgs,
        link::{activate::LinkActivateArgs, create::LinkCreateArgs, suspend::LinkSuspendArgs},
        location::create::LocationCreateArgs,
    },
    state::{
        device::{Device, DeviceType},
        globalstate::GlobalState,
        link::{Link, LinkLinkType},
    },
};
use doublezero_telemetry::{
    entrypoint::process_instruction as telemetry_process_instruction,
    error::TelemetryError,
    instructions::{TelemetryInstruction, INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX},
    pda::derive_device_latency_samples_pda,
    processors::telemetry::{
        initialize_device_latency_samples::InitializeDeviceLatencySamplesArgs,
        write_device_latency_samples::WriteDeviceLatencySamplesArgs,
    },
    serviceability_program_id,
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentLevel,
    hash::Hash,
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    system_program,
    transaction::{Transaction, TransactionError},
};

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
            index: 0,
            bump_seed: 0,
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
            index: 0,
            bump_seed: 0,
            code: "".to_string(),
            name: "".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
        }
    }
}

pub trait DeviceCreateArgsExt {
    fn default() -> DeviceCreateArgs;
}

impl DeviceCreateArgsExt for DeviceCreateArgs {
    fn default() -> DeviceCreateArgs {
        DeviceCreateArgs {
            index: 0,
            bump_seed: 0,
            code: "".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Switch,
            public_ip: [0; 4],
            dz_prefixes: Vec::new(),
            metrics_publisher_pk: Pubkey::default(),
        }
    }
}

pub trait LinkCreateArgsExt {
    fn default() -> LinkCreateArgs;
}

impl LinkCreateArgsExt for LinkCreateArgs {
    fn default() -> LinkCreateArgs {
        LinkCreateArgs {
            index: 0,
            bump_seed: 0,
            code: "".to_string(),
            side_a_pk: Pubkey::default(),
            side_z_pk: Pubkey::default(),
            link_type: LinkLinkType::L3,
            bandwidth: 0,
            mtu: 0,
            delay_ns: 0,
            jitter_ns: 0,
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

    #[allow(dead_code)]
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

    #[allow(dead_code)]
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
                loc_id: 1,
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
            .create_and_activate_device(DeviceCreateArgs {
                index: 0,     // set by the helper
                bump_seed: 0, // set by the helper
                code: "origin_device".to_string(),
                contributor_pk: Pubkey::default(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 2, 3, 4],
                dz_prefixes: Vec::new(),
                metrics_publisher_pk: origin_device_agent_pk,
            })
            .await?;

        // Create and activate target device.
        let target_device_pk = self
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "target_device".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [5, 6, 7, 8],
                metrics_publisher_pk: Pubkey::new_unique(),
                ..DeviceCreateArgs::default()
            })
            .await?;

        // Create and activate link.
        let link_pk = self
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".to_string(),
                    side_a_pk: origin_device_pk,
                    side_z_pk: target_device_pk,
                    link_type: LinkLinkType::L3,
                    bandwidth: 1000,
                    mtu: 1500,
                    delay_ns: 10,
                    jitter_ns: 1,
                    ..LinkCreateArgs::default()
                },
                1,
                ([10, 1, 1, 0], 30),
            )
            .await?;

        Ok((
            origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
        ))
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

    #[allow(dead_code)]
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

    #[allow(clippy::too_many_arguments, dead_code)]
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

    #[allow(dead_code)]
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
    #[allow(dead_code)]
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
                DoubleZeroInstruction::InitGlobalState,
                vec![
                    AccountMeta::new(program_config_pubkey, false),
                    AccountMeta::new(global_state_pubkey, false),
                ],
            )
            .await?;

            let (global_config_pubkey, _) = get_globalconfig_pda(&program_id);
            execute_serviceability_instruction(
                &mut banks_client,
                &payer,
                recent_blockhash,
                program_id,
                DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                    local_asn: 65000,
                    remote_asn: 65001,
                    device_tunnel_block: ([10, 0, 0, 0], 24),
                    user_tunnel_block: ([10, 0, 0, 0], 24),
                    multicastgroup_block: ([224, 0, 0, 0], 4),
                }),
                vec![
                    AccountMeta::new(global_config_pubkey, false),
                    AccountMeta::new(global_state_pubkey, false),
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
        let banks_client = banks_client.clone();
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
        let global_state = GlobalState::from(&account.data[..]);
        Ok(global_state.account_index + 1)
    }

    pub async fn create_location(
        &mut self,
        location: LocationCreateArgs,
    ) -> Result<Pubkey, BanksClientError> {
        let mut location = location;
        if location.index == 0 {
            location.index = self.get_next_global_state_index().await?;
        }
        let (location_pubkey, bump_seed) = get_location_pda(&self.program_id, location.index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                index: location.index,
                bump_seed,
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
        let mut exchange = exchange;
        if exchange.index == 0 {
            exchange.index = self.get_next_global_state_index().await?;
        }
        let (exchange_pubkey, bump_seed) = get_exchange_pda(&self.program_id, exchange.index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                index: exchange.index,
                bump_seed,
                code: exchange.code,
                name: exchange.name,
                lat: exchange.lat,
                lng: exchange.lng,
                loc_id: exchange.loc_id,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(exchange_pubkey)
    }

    pub async fn create_device(
        &mut self,
        device: DeviceCreateArgs,
    ) -> Result<Pubkey, BanksClientError> {
        let mut device = device;
        if device.index == 0 {
            device.index = self.get_next_global_state_index().await?;
        }
        let (device_pk, bump_seed) = get_device_pda(&self.program_id, device.index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: device.index,
                bump_seed,
                code: device.code,
                contributor_pk: device.contributor_pk,
                location_pk: device.location_pk,
                exchange_pk: device.exchange_pk,
                device_type: device.device_type,
                public_ip: device.public_ip,
                dz_prefixes: device.dz_prefixes,
                metrics_publisher_pk: device.metrics_publisher_pk,
            }),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new_readonly(device.location_pk, false),
                AccountMeta::new_readonly(device.exchange_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(device_pk)
    }

    pub async fn activate_device(&mut self, device_pk: Pubkey) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs),
            vec![
                AccountMeta::new(device_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    #[allow(dead_code)]
    pub async fn get_device(&mut self, pubkey: Pubkey) -> Result<Device, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let device = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Device::from(&device.data[..]))
    }

    #[allow(dead_code)]
    pub async fn suspend_device(&mut self, pubkey: Pubkey) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs {}),
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
    ) -> Result<Pubkey, BanksClientError> {
        let device_pk = self.create_device(device).await?;
        self.activate_device(device_pk).await?;
        Ok(device_pk)
    }

    pub async fn create_link(&mut self, link: LinkCreateArgs) -> Result<Pubkey, BanksClientError> {
        let mut link = link;
        if link.index == 0 {
            link.index = self.get_next_global_state_index().await?;
        }
        let (link_pk, bump_seed) = get_link_pda(&self.program_id, link.index);

        self.execute_transaction(
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                index: link.index,
                bump_seed,
                code: link.code,
                side_a_pk: link.side_a_pk,
                side_z_pk: link.side_z_pk,
                link_type: link.link_type,
                bandwidth: link.bandwidth,
                mtu: link.mtu,
                delay_ns: link.delay_ns,
                jitter_ns: link.jitter_ns,
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new_readonly(link.side_a_pk, false),
                AccountMeta::new_readonly(link.side_z_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await?;

        Ok(link_pk)
    }

    pub async fn activate_link(
        &mut self,
        link_pk: Pubkey,
        tunnel_id: u16,
        tunnel_net: ([u8; 4], u8),
    ) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id,
                tunnel_net,
            }),
            vec![
                AccountMeta::new(link_pk, false),
                AccountMeta::new(self.global_state_pubkey, false),
            ],
        )
        .await
    }

    #[allow(dead_code)]
    pub async fn get_link(&mut self, pubkey: Pubkey) -> Result<Link, BanksClientError> {
        let banks_client = {
            let context = self.context.lock().unwrap();
            context.banks_client.clone()
        };
        let link = banks_client.get_account(pubkey).await.unwrap().unwrap();
        Ok(Link::from(&link.data[..]))
    }

    #[allow(dead_code)]
    pub async fn suspend_link(&mut self, pubkey: Pubkey) -> Result<(), BanksClientError> {
        self.execute_transaction(
            DoubleZeroInstruction::SuspendLink(LinkSuspendArgs {}),
            vec![AccountMeta::new(pubkey, false)],
        )
        .await
    }

    pub async fn create_and_activate_link(
        &mut self,
        link: LinkCreateArgs,
        tunnel_id: u16,
        tunnel_net: ([u8; 4], u8),
    ) -> Result<Pubkey, BanksClientError> {
        let link_pk = self.create_link(link).await?;
        self.activate_link(link_pk, tunnel_id, tunnel_net).await?;
        Ok(link_pk)
    }

    pub async fn execute_transaction(
        &mut self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> Result<(), BanksClientError> {
        let (mut banks_client, payer, recent_blockhash) = {
            let context = self.context.lock().unwrap();
            (
                context.banks_client.clone(),
                context.payer.insecure_clone(),
                context.recent_blockhash,
            )
        };
        execute_serviceability_instruction(
            &mut banks_client,
            &payer,
            recent_blockhash,
            self.program_id,
            instruction,
            accounts,
        )
        .await
    }
}

#[allow(dead_code)]
pub async fn get_account_data(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<Account> {
    banks_client.get_account(pubkey).await.unwrap()
}

#[allow(dead_code)]
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
    let mut transaction = Transaction::new_with_payer(
        &[Instruction {
            program_id,
            accounts,
            data: instruction_data,
        }],
        Some(&payer.pubkey()),
    );
    transaction.sign(signers, recent_blockhash);
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
    program_test.add_program(
        "doublezero_telemetry",
        telemetry_program_id,
        processor!(telemetry_process_instruction),
    );

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
#[allow(dead_code)]
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
#[allow(dead_code)]
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
