use doublezero_sdk::{
    commands::user::check_access_pass::CheckUserAccessPassCommand, DoubleZeroClient, User,
    UserStatus,
};
use doublezero_serviceability::state::accesspass::AccessPass;
use log::info;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

pub fn process_access_pass_event(
    client: &dyn DoubleZeroClient,
    _pubkey: &Pubkey,
    accesspass: &AccessPass,
    users: &HashMap<Pubkey, User>,
    state_transitions: &mut HashMap<&'static str, usize>,
) -> eyre::Result<()> {
    let mut epoch = client.get_epoch()?;

    // For local test
    if epoch == 0 {
        epoch = 1;
    }

    // Try to disconnect users
    if accesspass.last_access_epoch < epoch {
        let users_to_disconnect = users.iter().filter(|(_, user)| {
            user.status == UserStatus::Activated && user.client_ip == accesspass.client_ip
        });

        for (user_pubkey, _user) in users_to_disconnect {
            let res = CheckUserAccessPassCommand {
                user_pubkey: *user_pubkey,
            }
            .execute(client);
            *state_transitions.entry("user-out-of-credits").or_insert(0) += 1;

            if res.is_ok() {
                info!(
                    "User {} suspended successfully {}",
                    user_pubkey,
                    res.unwrap()
                );
            } else {
                info!("Failed to suspend user {}: {:?}", user_pubkey, res.err());
            }
        }
    } else if accesspass.last_access_epoch > epoch {
        let users_to_reactivate = users.iter().filter(|(_, user)| {
            user.status == UserStatus::OutOfCredits && user.client_ip == accesspass.client_ip
        });

        for (user_pubkey, _user) in users_to_reactivate {
            let res = CheckUserAccessPassCommand {
                user_pubkey: *user_pubkey,
            }
            .execute(client);
            *state_transitions.entry("user-out-of-credits").or_insert(0) += 1;

            if res.is_ok() {
                info!(
                    "User {} reactivated successfully {}",
                    user_pubkey,
                    res.unwrap()
                );
            } else {
                info!("Failed to reactivate user {}: {:?}", user_pubkey, res.err());
            }
        }
    }

    Ok(())
}
