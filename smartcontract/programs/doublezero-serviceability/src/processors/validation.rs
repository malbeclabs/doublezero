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
        if let Some(expected_pda) = $pda {
            assert_eq!(account.key, expected_pda, "Invalid {label} PDA");
        }
    }};
}

pub(crate) use validate_program_account;
