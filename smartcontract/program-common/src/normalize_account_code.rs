/// Perform input sanitization on account codes with a single heap allocation for the output string
/// to minimize the overhead when using the validation in instruction handlers
pub fn normalize_account_code(val: &str) -> Result<String, &'static str> {
    val.chars()
        .try_fold(String::with_capacity(val.len()), |mut code, char| {
            if char.is_alphanumeric()
                || char.is_whitespace()
                || char == '_'
                || char == '-'
                || char == ':'
            {
                code.push(if char.is_whitespace() { '_' } else { char })
            } else {
                return Err("name must be alphanumeric");
            }
            Ok(code)
        })
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_valid_code() {
        let input = "myDevice01".to_string();
        let output = normalize_account_code(&input).unwrap();
        assert_eq!(output, input);
    }

    #[test]
    fn test_invalid_code() {
        let input = "myDevice/2".to_string();
        assert_eq!(
            normalize_account_code(&input),
            Err("name must be alphanumeric")
        );
    }

    #[test]
    fn test_replace_code_whitespace() {
        let input = "my Device 3".to_string();
        let output = normalize_account_code(&input).unwrap();
        assert_eq!(output, "my_Device_3".to_string());
    }
}
