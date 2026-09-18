use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::accesspass::close::CloseAccessPassArgs, state::accesspass::AccessPassKind,
};
use doublezero_serviceability_instruction::accesspass::close_access_pass;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Closes an access pass. `kind` names the kind of pass the caller means to close; the program
/// refuses the call when the stored pass is a different kind, so `kind` must carry the caller's
/// intent rather than a value read back from the pass.
#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccessPassCommand {
    pub pubkey: Pubkey,
    pub kind: AccessPassKind,
}

impl CloseAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(close_access_pass(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            self.kind,
            CloseAccessPassArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::close::CloseAccessPassCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda, processors::accesspass::close::CloseAccessPassArgs,
        state::accesspass::AccessPassKind,
    };
    use doublezero_serviceability_instruction::accesspass::close_access_pass;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_close_accesspass_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let (pda_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

        let expected = close_access_pass(
            &program_id,
            &payer,
            &pda_pubkey,
            AccessPassKind::EdgeSeat,
            CloseAccessPassArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CloseAccessPassCommand {
            pubkey: pda_pubkey,
            kind: AccessPassKind::EdgeSeat,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_close_accesspass_command_threads_a_different_kind() {
        // A second, distinct kind from the test above: proves the command reads
        // self.kind rather than passing through a hardcoded value.
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let (pda_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

        let expected = close_access_pass(
            &program_id,
            &payer,
            &pda_pubkey,
            AccessPassKind::SolanaValidator,
            CloseAccessPassArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CloseAccessPassCommand {
            pubkey: pda_pubkey,
            kind: AccessPassKind::SolanaValidator,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
