use doublezero_serviceability::pda::get_permission_pda;
use solana_sdk::instruction::Instruction;

use crate::DoubleZeroClient;

/// Remove the payer's Permission PDA that the instruction builder appended as the last account.
///
/// Only for the feed-subscription instructions (`SubscribeFeed` / `UnsubscribeFeed`), whose
/// processor authorizes the access pass's own `user_payer` before it ever consults
/// `authorize()`. For that payer the account is never read, and it costs 33 bytes of packet:
/// enough to push the worst-case single-feed leave past the legacy transaction limit.
pub(crate) fn drop_payer_permission_account(client: &dyn DoubleZeroClient, ix: &mut Instruction) {
    let (permission_pda, _) = get_permission_pda(&client.get_program_id(), &client.get_payer());
    if ix
        .accounts
        .last()
        .is_some_and(|meta| meta.pubkey == permission_pda)
    {
        ix.accounts.pop();
    }
}
