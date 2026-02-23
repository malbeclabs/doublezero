/// Perform input sanitization on account codes with a single heap allocation for the output string
/// to minimize the overhead when using the validation in instruction handlers
pub fn validate_account_code(val: &str) -> Result<String, &'static str> {
    val.chars()
        .try_fold(String::with_capacity(val.len()), |mut code, char| {
            if char.is_alphanumeric() || char == ':' || char == '_' || char == '-' {
                code.push(char);
            } else {
                return Err("name must be alphanumeric, `_`, `-`, or `:` only");
            }
            Ok(code)
        })
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_valid_code() {
        let input = "my_device:-01".to_string();
        let output = validate_account_code(&input).unwrap();
        assert_eq!(output, input);
    }

    #[test]
    fn test_valid_code_preserves_case() {
        let input = "My_Device:-01".to_string();
        let output = validate_account_code(&input).unwrap();
        assert_eq!(output, input);
    }

    #[test]
    fn test_invalid_code() {
        let input = "myDevice/2".to_string();
        let err = Err("name must be alphanumeric, `_`, `-`, or `:` only");
        assert_eq!(validate_account_code(&input), err,);

        let input = "myDevice 3".to_string();
        assert_eq!(validate_account_code(&input), err);

        let input = "myDevice@3".to_string();
        assert_eq!(validate_account_code(&input), err);
    }
}
