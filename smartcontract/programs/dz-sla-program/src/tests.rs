#[cfg(test)]
pub mod test {
    use crate::entrypoint::*;
    use crate::instructions::*;
    use crate::pda::*;
    use crate::processors::device::create::*;
    use crate::processors::exchange::create::*;
    use crate::processors::globalconfig::set::SetGlobalConfigArgs;
    use crate::processors::location::create::*;
    use crate::processors::tunnel::{activate::*, create::*};
    use crate::processors::user::{activate::*, create::*};
    use crate::state::accountdata::AccountData;
    use std::any::type_name;

    use crate::state::accounttype::AccountType;
    use crate::state::globalstate::GlobalState;
    use crate::{
        state::{device::*, location::*, tunnel::*, user::*},
        types::*,
    };

    use borsh::to_vec;
    use solana_program_test::*;
    use solana_sdk::{
        instruction::{AccountMeta, Instruction},
        pubkey::Pubkey,
        signature::{Keypair, Signer},
        system_program,
        transaction::Transaction,
    };

    #[tokio::test]
    async fn test_doublezero_program() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "double_zero_sla_program",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start...");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 65000,
                remote_asn: 65001,
                tunnel_tunnel_block: ([10, 0, 0, 0], 24),
                user_tunnel_block: ([10, 0, 0, 0], 24),
            }),
            vec![AccountMeta::new(globalconfig_pubkey, false)],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        // Location _la

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("Testing Location LA initialization...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let location_la_code = "la".to_string();
        let (location_la_pubkey, _location_bump_seed) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);
        let location_la: LocationCreateArgs = LocationCreateArgs {
            index: globalstate_account.account_index + 1,
            code: location_la_code.clone(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        };

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location_la),
            vec![
                AccountMeta::new(location_la_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location_la  =
            get_account_data(&mut banks_client, location_la_pubkey)
                .await
                .expect("Unable to get Location")
                .get_location();
        assert_eq!(location_la.account_type, AccountType::Location);
        assert_eq!(location_la.code, location_la_code);
        assert_eq!(location_la.status, LocationStatus::Activated);

        println!(
            "✅ Location LA initialized successfully with index: {}",
            location_la.index
        );

        println!("Testing Location NY initialization...");
        // Location _la

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 1);

        let location_ny_code = "ny".to_string();
        let (location_ny_pubkey, _location_bump_seed) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);
        let location_ny: LocationCreateArgs = LocationCreateArgs {
            index: globalstate_account.account_index + 1,
            code: location_ny_code.clone(),
            name: "New York".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        };

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location_ny),
            vec![
                AccountMeta::new(location_ny_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location_ny =
            get_account_data(&mut banks_client, location_ny_pubkey)
                .await
                .expect("Unable to get Location")
                .get_location();
        assert_eq!(location_ny.account_type, AccountType::Location);
        assert_eq!(location_ny.code, location_ny_code);
        println!(
            "✅ Location initialized successfully with index: {}",
            location_ny.index
        );

        /***********************************************************************************************************************************/

        // Exchange _la
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let exchange_la_code = "la".to_string();
        let (exchange_la_pubkey, _exchange_bump_seed) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);
        let exchange_la: ExchangeCreateArgs = ExchangeCreateArgs {
            index: globalstate_account.account_index + 1,
            code: exchange_la_code.clone(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        };

        println!("Testing Exchange LA initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange_la),
            vec![
                AccountMeta::new(exchange_la_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange_la =
            get_account_data(&mut banks_client, exchange_la_pubkey)
                .await
                .expect("Unable to get Exchange")
                .get_exchange();
        assert_eq!(exchange_la.account_type, AccountType::Exchange);
        assert_eq!(exchange_la.code, exchange_la_code);
        println!(
            "✅ Exchange initialized successfully with index: {}",
            exchange_la.index
        );

        println!("Testing Exchange NY initialization...");
        // Exchange _ny
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);

        let exchange_ny_code = "ny".to_string();
        let (exchange_ny_pubkey, _exchange_bump_seed) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);
        let exchange_ny: ExchangeCreateArgs = ExchangeCreateArgs {
            index: globalstate_account.account_index + 1,
            code: exchange_ny_code.clone(),
            name: "New York".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        };

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange_ny),
            vec![
                AccountMeta::new(exchange_ny_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange_ny = get_account_data(&mut banks_client, exchange_ny_pubkey)
                .await
                .expect("Unable to get Exchange")
                .get_exchange();
        assert_eq!(exchange_ny.account_type, AccountType::Exchange);
        assert_eq!(exchange_ny.code, exchange_ny_code);
        println!(
            "✅ Exchange initialized successfully with index: {}",
            exchange_ny.index
        );

        /***********************************************************************************************************************************/

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 4);

        // Device _la
        let device_la_code = "la1".to_string();
        let (device_la_pubkey, _exchange_bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);
        let device_la: DeviceCreateArgs = DeviceCreateArgs {
            index: globalstate_account.account_index + 1,
            code: device_la_code.clone(),
            location_pk: location_la_pubkey,
            exchange_pk: exchange_la_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 0, 0, 1],
            dz_ef_pools: vec!(networkv4_parse(&"10.0.0.1/24".to_string())),
        };

        println!("Testing Device LA initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(device_la),
            vec![
                AccountMeta::new(device_la_pubkey, false),
                AccountMeta::new(location_la_pubkey, false),
                AccountMeta::new(exchange_la_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_la_pubkey)
            .await
            .expect("Unable to get Device")
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, device_la_code);
        println!(
            "✅ Device LA initialized successfully with index: {}",
            device_la.index
        );

        println!("Testing Device NY initialization...");
        // Device _ny
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 5);

        let device_ny_code = "ny1".to_string();
        let (device_ny_pubkey, _exchange_bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);
        let device_ny: DeviceCreateArgs = DeviceCreateArgs {
            index: globalstate_account.account_index + 1,
            code: device_ny_code.clone(),
            location_pk: location_ny_pubkey,
            exchange_pk: exchange_ny_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 0, 0, 2],
            dz_ef_pools: vec!(networkv4_parse(&"10.1.0.1/24".to_string())),
        };

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(device_ny),
            vec![
                AccountMeta::new(device_ny_pubkey, false),
                AccountMeta::new(location_ny_pubkey, false),
                AccountMeta::new(exchange_ny_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_ny = get_account_data(&mut banks_client, device_ny_pubkey)
            .await
            .expect("Unable to get Device")
            .get_device();
        assert_eq!(device_ny.account_type, AccountType::Device);
        assert_eq!(device_ny.code, device_ny_code);
        println!(
            "✅ Device NY initialized successfully with index: {}",
            device_ny.index
        );

        /***********************************************************************************************************************************/

        // Device _la
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 6);

        let tunnel_la_ny_code = "la-ny1".to_string();
        let (tunnel_la_ny_pubkey, _exchange_bump_seed) =
            get_tunnel_pda(&program_id, globalstate_account.account_index + 1);
        let tunnel_la_ny: TunnelCreateArgs = TunnelCreateArgs {
            index: globalstate_account.account_index + 1,
            code: tunnel_la_ny_code.clone(),
            side_a_pk: device_la_pubkey,
            side_z_pk: device_ny_pubkey,
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 100,
            mtu: 1900,
            delay_ns: 12 * 1000000,
            jitter_ns: 1 * 1000000,
        };

        println!("Testing Tunnel LA-NY initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateTunnel(tunnel_la_ny),
            vec![
                AccountMeta::new(tunnel_la_ny_pubkey, false),
                AccountMeta::new(device_la_pubkey, false),
                AccountMeta::new(device_ny_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        // Check account data
        let tunnel = get_account_data(&mut banks_client, tunnel_la_ny_pubkey)
            .await
            .expect("Unable to get Tunnel")
            .get_tunnel();
        assert_eq!(tunnel.account_type, AccountType::Tunnel);
        assert_eq!(tunnel.code, tunnel_la_ny_code);
        assert_eq!(tunnel.status, TunnelStatus::Pending);
        println!(
            "✅ Tunnel LA-NY initialized successfully with index: {}",
            tunnel.index
        );

        println!("Testing Tunnel activation...");
        let tunnel_net: NetworkV4 = networkv4_parse(&"10.31.0.0/31".to_string());
        let tunnel_activate: TunnelActivateArgs = TunnelActivateArgs {
            index: tunnel.index,
            tunnel_id: 1,
            tunnel_net,
        };

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateTunnel(tunnel_activate),
            vec![
                AccountMeta::new(tunnel_la_ny_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(payer.pubkey(), false),
            ],
            &payer,
        )
        .await;

        // Check account data
        let tunnel = get_account_data(&mut banks_client, tunnel_la_ny_pubkey)
            .await
            .expect("Unable to get Tunnel")
            .get_tunnel();
        assert_eq!(tunnel.account_type, AccountType::Tunnel);
        assert_eq!(tunnel.code, tunnel_la_ny_code);
        assert_eq!(tunnel.status, TunnelStatus::Activated);
        println!(
            "✅ Tunnel LA-NY activated successfully with value: {:?}",
            tunnel_net
        );

        println!("Start Users...");
        /***********************************************************************************************************************************/

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 7);

        // User 100.0.0.1
        let user_ip = [100, 0, 0, 1];
        let (user1_pubkey, _user_bump_seed) =
            get_user_pda(&program_id, globalstate_account.account_index + 1);
        let user1: UserCreateArgs = UserCreateArgs {
            index: globalstate_account.account_index + 1,
            user_type: UserType::Server,
            device_pk: device_la_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip.clone(),
        };

        println!("Testing User1 initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateUser(user1),
            vec![
                AccountMeta::new(user1_pubkey, false),
                AccountMeta::new(device_la_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        // Check account data
        let user1= get_account_data(&mut banks_client, user1_pubkey)
            .await
            .expect("Unable to get User")
            .get_user();
        assert_eq!(user1.account_type, AccountType::User);
        assert_eq!(user1.status, UserStatus::Pending);
        println!(
            "✅ User initialized successfully with index: {}",
            user1.index
        );

        let tunnel_net: NetworkV4 = networkv4_parse(&"10.0.0.0/24".to_string());
        let dz_ip: IpV4 = ipv4_parse(&"10.2.0.1".to_string());

        let update1: UserActivateArgs = UserActivateArgs {
            index: user1.index,
            tunnel_id: 1,
            tunnel_net,
            dz_ip,
        };

        println!("Testing User1 activation...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateUser(update1),
            vec![
                AccountMeta::new(user1_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        // Check account data
        let user1 = get_account_data(&mut banks_client, user1_pubkey)
            .await
            .expect("Unable to get User")
            .get_user();
        assert_eq!(user1.account_type, AccountType::User);
        assert_eq!(user1.tunnel_id, 1);
        assert_eq!(user1.tunnel_net, tunnel_net);
        assert_eq!(user1.dz_ip, dz_ip);
        assert_eq!(user1.status, UserStatus::Activated);
        println!(
            "✅ User initialized successfully with index: {}",
            user1.index
        );
    }

    /**************************************************************************************************************************/

    pub async fn get_globalstate(
        banks_client: &mut BanksClient,
        globalstate_pubkey: Pubkey,
    ) -> GlobalState {
        match banks_client.get_account(globalstate_pubkey).await {
            Ok(account) => match account {
                Some(account_data) => {
                    let globalstate = GlobalState::from(&account_data.data[..]);
                    assert_eq!(globalstate.account_type, AccountType::GlobalState);

                    println!("⬅️  Read {:?}", globalstate);

                    globalstate
                }
                None => panic!("GlobalState account not found"),
            },
            Err(err) => panic!("GlobalState account not found: {:?}", err),
        }
    }

    pub fn get_type_name<T>() -> String {
        let full_type_name = type_name::<T>();
        if let Some(last_name) = full_type_name.rsplit("::").next() {
            return last_name.to_string();
        }

        "".to_string()
    }

    pub async fn get_account_data(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<AccountData> {
        print!("⬅️  Read: ");

        match banks_client.get_account(pubkey).await {
            Ok(account) => match account {
                Some(account_data) => {
                    let object = AccountData::from(&account_data.data[..]);
                    println!("{:?}", object);

                    Some(object)
                }
                None => None,
            },
            Err(err) => panic!("account not found: {:?}", err),
        }
    }

    pub async fn execute_transaction(
        banks_client: &mut BanksClient,
        recent_blockhash: solana_program::hash::Hash,
        program_id: Pubkey,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
        payer: &Keypair,
    ) {
        print!("➡️  Transaction {:?} ", instruction);

        let mut transaction = create_transaction(program_id, instruction, accounts, payer);
        transaction.sign(&[&payer], recent_blockhash);
        banks_client.process_transaction(transaction).await.unwrap();

        println!("✅")
    }

    pub fn create_transaction(
        program_id: Pubkey,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
        payer: &Keypair,
    ) -> Transaction {
        return Transaction::new_with_payer(
            &[Instruction::new_with_bytes(
                program_id,
                &to_vec(&instruction).unwrap(),
                [
                    accounts,
                    vec![
                        AccountMeta::new(payer.pubkey(), true),
                        AccountMeta::new(system_program::id(), false),
                    ],
                ]
                .concat(),
            )],
            Some(&payer.pubkey()),
        );
    }
}
