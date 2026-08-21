use doublezero_serviceability::pda::get_permission_pda;
use solana_sdk::instruction::{AccountMeta, Instruction};

use crate::DoubleZeroClient;

pub(crate) fn append_payer_permission_account(client: &dyn DoubleZeroClient, ix: &mut Instruction) {
    let program_id = client.get_program_id();
    let (permission_pda, _) = get_permission_pda(&program_id, &client.get_payer());
    if let Ok(account) = client.get_account(permission_pda) {
        if account.owner == program_id {
            ix.accounts
                .push(AccountMeta::new_readonly(permission_pda, false));
        }
    }
}
