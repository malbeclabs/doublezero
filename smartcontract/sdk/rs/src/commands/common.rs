use doublezero_serviceability::pda::get_permission_pda;
use solana_sdk::instruction::{AccountMeta, Instruction};

use crate::DoubleZeroClient;

pub(crate) fn append_payer_permission_account(
    client: &dyn DoubleZeroClient,
    ix: &mut Instruction,
) -> eyre::Result<()> {
    let program_id = client.get_program_id();
    let (permission_pda, _) = get_permission_pda(&program_id, &client.get_payer());
    if let Some(account) = client
        .get_multiple_accounts(vec![permission_pda])?
        .into_iter()
        .flatten()
        .next()
    {
        if account.owner == program_id {
            ix.accounts
                .push(AccountMeta::new_readonly(permission_pda, false));
        }
    }
    Ok(())
}
