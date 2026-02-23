use doublezero_program_common::{types::parse_utils::bandwidth_parse, validate_account_code};
use solana_sdk::pubkey::Pubkey;

pub fn validate_code(val: &str) -> Result<String, String> {
    validate_account_code(val).map_err(String::from)
}

pub fn validate_pubkey(val: &str) -> Result<String, String> {
    if val.eq("me") {
        return Ok(val.to_string());
    }
    match val.parse::<Pubkey>() {
        Ok(_) => Ok(val.to_string()),
        Err(_) => Err(String::from("invalid pubkey format")),
    }
}

pub fn validate_pubkey_or_code(val: &str) -> Result<String, String> {
    val.parse::<Pubkey>()
        .map(|pubkey| pubkey.to_string())
        .or_else(|_| validate_code(val).map_err(|_| "invalid pubkey or code format".to_string()))
}

pub fn validate_parse_bandwidth(val: &str) -> Result<u64, String> {
    if bandwidth_parse(val).is_ok() {
        bandwidth_parse(val)
    } else {
        Err(String::from("invalid bandwidth format"))
    }
}

pub fn validate_parse_mtu(val: &str) -> Result<u32, String> {
    if let Ok(mtu) = val.parse::<u32>() {
        if (2048..=10218).contains(&mtu) {
            Ok(mtu)
        } else {
            Err(String::from("MTU must be between 2048 and 10218"))
        }
    } else {
        Err(String::from("invalid MTU format"))
    }
}

pub fn validate_parse_delay_ms(val: &str) -> Result<f64, String> {
    if let Ok(delay) = val.parse::<f64>() {
        if (0.01..=1000.0).contains(&delay) {
            Ok(delay)
        } else {
            Err(String::from("Delay must be between 0.01 and 1000 ms"))
        }
    } else {
        Err(String::from("invalid delay format"))
    }
}

pub fn validate_parse_jitter_ms(val: &str) -> Result<f64, String> {
    if let Ok(jitter) = val.parse::<f64>() {
        if (0.01..=1000.0).contains(&jitter) {
            Ok(jitter)
        } else {
            Err(String::from("Jitter must be between 0.01 and 1000 ms"))
        }
    } else {
        Err(String::from("invalid jitter format"))
    }
}

pub fn validate_parse_delay_override_ms(val: &str) -> Result<f64, String> {
    if let Ok(delay) = val.parse::<f64>() {
        if (delay == 0.0) || (0.01..=1000.0).contains(&delay) {
            Ok(delay)
        } else {
            Err(String::from(
                "Delay override must be 0 (disabled) or between 0.01 and 1000 ms",
            ))
        }
    } else {
        Err(String::from("invalid delay override format"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_validate_code() {
        assert!(validate_code("abc_123-:XYZ").is_ok());
        assert!(validate_code("abc@123").is_err());
        assert!(validate_code("abc 123-:XYZ").is_err());
    }

    #[test]
    fn test_validate_pubkey() {
        let pk = Pubkey::new_unique().to_string();
        assert!(validate_pubkey(&pk).is_ok());
        assert!(validate_pubkey("me").is_ok());
        assert!(validate_pubkey("not_a_pubkey").is_err());
    }

    #[test]
    fn test_validate_pubkey_or_code() {
        let pk = Pubkey::new_unique().to_string();
        assert!(validate_pubkey_or_code(&pk).is_ok());
        assert!(validate_pubkey_or_code("valid_code-123").is_ok());
        assert!(validate_pubkey_or_code("invalid code!").is_err());
    }

    #[test]
    fn test_validate_bandwidth() {
        assert!(validate_parse_bandwidth("100Mbps").is_ok());
        assert!(validate_parse_bandwidth("1Gbps").is_ok());
        assert!(validate_parse_bandwidth("500Kbps").is_ok());
        assert!(validate_parse_bandwidth("200bps").is_ok());
        assert!(validate_parse_bandwidth("invalid").is_err());
        assert!(validate_parse_bandwidth("1000").is_err());
        assert!(validate_parse_bandwidth("0").is_err());
    }

    #[test]
    fn test_validate_mtu() {
        assert!(validate_parse_mtu("2048").is_ok());
        assert!(validate_parse_mtu("10218").is_ok());
        assert!(validate_parse_mtu("2047").is_err());
        assert!(validate_parse_mtu("10219").is_err());
        assert!(validate_parse_mtu("not_a_number").is_err());
    }

    #[test]
    fn test_validate_delay_ms() {
        assert!(validate_parse_delay_ms("0.01").is_ok());
        assert!(validate_parse_delay_ms("1").is_ok());
        assert!(validate_parse_delay_ms("1000").is_ok());
        assert!(validate_parse_delay_ms("0.009").is_err());
        assert!(validate_parse_delay_ms("1001").is_err());
        assert!(validate_parse_delay_ms("not_a_number").is_err());
    }

    #[test]
    fn test_validate_jitter_ms() {
        assert!(validate_parse_jitter_ms("1").is_ok());
        assert!(validate_parse_jitter_ms("0.5").is_ok());
        assert!(validate_parse_jitter_ms("1000").is_ok());
        assert!(validate_parse_jitter_ms("0").is_err());
        assert!(validate_parse_jitter_ms("0.0001").is_err());
        assert!(validate_parse_jitter_ms("1001").is_err());
        assert!(validate_parse_jitter_ms("not_a_number").is_err());
    }

    #[test]
    fn test_validate_delay_override_ms() {
        assert!(validate_parse_delay_override_ms("0").is_ok());
        assert!(validate_parse_delay_override_ms("0.01").is_ok());
        assert!(validate_parse_delay_override_ms("1").is_ok());
        assert!(validate_parse_delay_override_ms("1000").is_ok());
        assert!(validate_parse_delay_override_ms("0.009").is_err());
        assert!(validate_parse_delay_override_ms("1001").is_err());
        assert!(validate_parse_delay_override_ms("not_a_number").is_err());
    }
}
