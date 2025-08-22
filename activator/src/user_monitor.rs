use doublezero_sdk::{
    commands::{
        accesspass::list::ListAccessPassCommand,
        user::{delete::DeleteUserCommand, list::ListUserCommand},
    },
    DZClient, User,
};
use doublezero_serviceability::{pda::get_accesspass_pda, state::accesspass::AccessPass};
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::HashMap,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    thread,
    time::Duration,
};

pub fn process_user_monitor_thread(
    rpc_url: String,
    websocket_url: String,
    program_id: String,
    keypair: PathBuf,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    info!("User monitor thread started");

    let client = DZClient::new(
        Some(rpc_url.clone()),
        Some(websocket_url.clone()),
        Some(program_id.clone()),
        Some(keypair.clone()),
    )?;

    while !stop_signal.load(Ordering::Relaxed) {
        // Monitor users and perform necessary actions

        let epoch = client.get_epoch()?;
        let program_id = client.get_program_id();

        // Read data on-chain
        let users = ListUserCommand.execute(&client)?;
        let accesspass = ListAccessPassCommand.execute(&client)?;

        // Get users to disconnect
        let users_to_disconnect = get_users_to_disconnect(epoch, &users, &accesspass, program_id)?;

        if !users_to_disconnect.is_empty() {
            info!("users_to_disconnect: {users_to_disconnect:?}");
            // Disconnect users

            for user in users_to_disconnect {
                let res = DeleteUserCommand { pubkey: user }.execute(&client);

                if res.is_ok() {
                    info!("User {} disconnected successfully {}", user, res.unwrap());
                } else {
                    info!("Failed to disconnect user {}: {:?}", user, res.err());
                }
            }
        }

        // Sleep for a while before the next iteration
        thread::sleep(Duration::from_secs(crate::constants::SLEEP_DURATION_SECS));
    }

    Ok(())
}

fn get_users_to_disconnect(
    epoch: u64,
    users: &HashMap<Pubkey, User>,
    accesspass: &HashMap<Pubkey, AccessPass>,
    program_id: &Pubkey,
) -> eyre::Result<Vec<Pubkey>> {
    // Implement user review logic here
    let mut users_to_disconnect = Vec::new();

    // Example logic: just print the users and access passes
    for (user_pk, user) in users {
        let (access_pk, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);

        let access = accesspass.get(&access_pk);

        if let Some(access) = access {
            if access.last_access_epoch < epoch {
                users_to_disconnect.push(*user_pk);
            }
        }
    }

    Ok(users_to_disconnect)
}

#[cfg(test)]
mod tests {
    use crate::user_monitor::get_users_to_disconnect;
    use doublezero_sdk::{AccountType, User, UserType};
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::accesspass::{AccessPass, AccessPassStatus, AccessPassType},
    };
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_user_monitor() {
        let mut users = HashMap::new();
        let mut accesspass = HashMap::new();

        let program_id = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let user1_pk = Pubkey::new_unique();
        let user1 = User {
            account_type: AccountType::User,
            owner: user_payer,
            index: 0,
            bump_seed: 255,
            client_ip: [10, 0, 0, 1].into(),
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::new_unique(),
            device_pk: Pubkey::new_unique(),
            dz_ip: [10, 0, 0, 1].into(),
            cyoa_type: doublezero_sdk::UserCYOA::GREOverDIA,
            tunnel_id: 500,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: doublezero_sdk::UserStatus::Activated,
            publishers: Vec::new(),
            subscribers: Vec::new(),
            validator_pubkey: Pubkey::default(),
        };
        users.insert(user1_pk, user1.clone());

        let (accesspass1_pk, bump_seed) =
            get_accesspass_pda(&program_id, &user1.client_ip, &user_payer);
        let accesspass1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user1.client_ip,
            owner: Pubkey::new_unique(),
            status: AccessPassStatus::Requested,
            last_access_epoch: 15,
            connection_count: 1,
            user_payer,
        };
        accesspass.insert(accesspass1_pk, accesspass1);

        let users_to_disconnect = get_users_to_disconnect(10, &users, &accesspass, &program_id);

        assert!(users_to_disconnect.is_ok());
        assert!(users_to_disconnect.unwrap().is_empty());

        let users_to_disconnect = get_users_to_disconnect(20, &users, &accesspass, &program_id);

        assert!(users_to_disconnect.is_ok());
        let users_to_disconnect = users_to_disconnect.unwrap();
        assert_eq!(users_to_disconnect.len(), 1);
        assert_eq!(users_to_disconnect[0], user1_pk);
    }
}
