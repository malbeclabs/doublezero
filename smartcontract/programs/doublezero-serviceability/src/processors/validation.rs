/// Validate that an account is owned by the program, non-empty, optionally writable,
/// and optionally matches an expected PDA.
///
/// Uses a macro so that assertion panics report the caller's file/line.
macro_rules! validate_program_account {
    ($account:expr, $program_id:expr, writable = $writable:expr, pda = $pda:expr, $label:expr) => {{
        let account = $account;
        let program_id = $program_id;
        let label = $label;
        assert_eq!(account.owner, program_id, "Invalid {label} Account Owner");
        assert!(!account.data_is_empty(), "{label} Account is empty");
        if $writable {
            assert!(account.is_writable, "{label} Account is not writable");
        }
        assert_eq!(account.key, $pda, "Invalid {label} PDA");
    }};
    ($account:expr, $program_id:expr, writable = $writable:expr, $label:expr) => {{
        let account = $account;
        let program_id = $program_id;
        let label = $label;
        assert_eq!(account.owner, program_id, "Invalid {label} Account Owner");
        assert!(!account.data_is_empty(), "{label} Account is empty");
        if $writable {
            assert!(account.is_writable, "{label} Account is not writable");
        }
    }};
}

pub(crate) use validate_program_account;

#[cfg(test)]
mod tests {
    use solana_program::{account_info::AccountInfo, clock::Epoch, pubkey::Pubkey};

    fn make_account_info<'a>(
        key: &'a Pubkey,
        is_writable: bool,
        lamports: &'a mut u64,
        data: &'a mut [u8],
        owner: &'a Pubkey,
    ) -> AccountInfo<'a> {
        AccountInfo::new(
            key,
            false,
            is_writable,
            lamports,
            data,
            owner,
            false,
            Epoch::default(),
        )
    }

    #[test]
    fn test_validate_program_account_valid() {
        let program_id = Pubkey::new_unique();
        let key = Pubkey::new_unique();
        let mut lamports = 1000u64;
        let mut data = vec![1u8; 8];

        let account = make_account_info(&key, true, &mut lamports, &mut data, &program_id);

        // Should not panic
        validate_program_account!(&account, &program_id, writable = true, pda = &key, "Test");
    }

    #[test]
    #[should_panic(expected = "Invalid Test PDA")]
    fn test_validate_program_account_wrong_pda_panics() {
        let program_id = Pubkey::new_unique();
        let key = Pubkey::new_unique();
        let wrong_pda = Pubkey::new_unique();
        let mut lamports = 1000u64;
        let mut data = vec![1u8; 8];

        let account = make_account_info(&key, true, &mut lamports, &mut data, &program_id);

        validate_program_account!(
            &account,
            &program_id,
            writable = true,
            pda = &wrong_pda,
            "Test"
        );
    }

    #[test]
    #[should_panic(expected = "Invalid Test Account Owner")]
    fn test_validate_program_account_wrong_owner_panics() {
        let program_id = Pubkey::new_unique();
        let wrong_owner = Pubkey::new_unique();
        let key = Pubkey::new_unique();
        let mut lamports = 1000u64;
        let mut data = vec![1u8; 8];

        let account = make_account_info(&key, true, &mut lamports, &mut data, &wrong_owner);

        validate_program_account!(&account, &program_id, writable = true, "Test");
    }

    #[test]
    #[should_panic(expected = "Test Account is empty")]
    fn test_validate_program_account_empty_data_panics() {
        let program_id = Pubkey::new_unique();
        let key = Pubkey::new_unique();
        let mut lamports = 1000u64;
        let mut data = vec![];

        let account = make_account_info(&key, true, &mut lamports, &mut data, &program_id);

        validate_program_account!(&account, &program_id, writable = true, "Test");
    }

    #[test]
    #[should_panic(expected = "Test Account is not writable")]
    fn test_validate_program_account_not_writable_panics() {
        let program_id = Pubkey::new_unique();
        let key = Pubkey::new_unique();
        let mut lamports = 1000u64;
        let mut data = vec![1u8; 8];

        let account = make_account_info(&key, false, &mut lamports, &mut data, &program_id);

        validate_program_account!(&account, &program_id, writable = true, "Test");
    }
}
