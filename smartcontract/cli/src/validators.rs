use doublezero_sdk::{
    bandwidth_parse, ipv4_parse, networkv4_list_parse, networkv4_parse, IpV4, NetworkV4,
    NetworkV4List,
};
use ipnetwork::Ipv4Network;
use solana_sdk::pubkey::Pubkey;

use crate::helpers::parse_pubkey;

pub fn validate_code(val: &str) -> Result<String, String> {
    if val
        .chars()
        .all(|c| c.is_alphanumeric() || c == '_' || c == '-' || c == ':')
    {
        Ok(val.to_string())
    } else {
        Err(String::from("name must be alphanumeric"))
    }
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
    match val.parse::<Pubkey>() {
        Ok(_) => Ok(val.to_string()),
        Err(_) => {
            if val
                .chars()
                .all(|c| c.is_alphanumeric() || c == '_' || c == '-')
                && !val.is_empty()
            {
                Ok(val.to_string())
            } else {
                Err(String::from("invalid pubkey or code format"))
            }
        }
    }
}

pub fn validate_parse_pubkey(val: &str) -> Result<Pubkey, String> {
    match parse_pubkey(val) {
        Some(pubkey) => Ok(pubkey),
        None => Err(String::from("invalid pubkey format")),
    }
}

pub fn validate_parse_ipv4(val: &str) -> Result<IpV4, String> {
    if val.parse::<std::net::Ipv4Addr>().is_ok() {
        let ip = ipv4_parse(val)?;
        Ok(ip)
    } else {
        Err(String::from("invalid IPv4 address format"))
    }
}

pub fn validate_parse_networkv4(val: &str) -> Result<NetworkV4, String> {
    networkv4_parse(val)
}

pub fn validate_parse_networkv4_list(val: &str) -> Result<NetworkV4List, String> {
    if val.split(',').all(|ip| ip.parse::<Ipv4Network>().is_ok()) {
        Ok(networkv4_list_parse(val)?)
    } else {
        Err(String::from("invalid networkv4 list format"))
    }
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
        if (1.0..=1000.0).contains(&delay) {
            Ok(delay)
        } else {
            Err(String::from("Delay must be between 1 and 1000 ms"))
        }
    } else {
        Err(String::from("invalid delay format"))
    }
}

pub fn validate_parse_jitter_ms(val: &str) -> Result<f64, String> {
    if let Ok(jitter) = val.parse::<f64>() {
        if (1.0..=1000.0).contains(&jitter) {
            Ok(jitter)
        } else {
            Err(String::from("Jitter must be between 1 and 1000 ms"))
        }
    } else {
        Err(String::from("invalid jitter format"))
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
    fn test_validate_ipv4() {
        assert!(validate_parse_ipv4("100.0.0.1").is_ok());
        assert!(validate_parse_ipv4("999.999.999.999").is_err());
    }

    #[test]
    fn test_validate_networkv4_list() {
        assert!(validate_parse_networkv4_list("192.168.1.0/24,10.0.0.0/8").is_ok());
        assert!(validate_parse_networkv4_list("192.168.1.0/24,not_a_network").is_err());
    }

    #[test]
    fn test_validate_bandwidth() {
        assert!(validate_parse_bandwidth("100Mbps").is_ok());
        assert!(validate_parse_bandwidth("invalid").is_err());
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
        assert!(validate_parse_delay_ms("1").is_ok());
        assert!(validate_parse_delay_ms("1000").is_ok());
        assert!(validate_parse_delay_ms("0.5").is_err());
        assert!(validate_parse_delay_ms("1001").is_err());
        assert!(validate_parse_delay_ms("not_a_number").is_err());
    }

    #[test]
    fn test_validate_jitter_ms() {
        assert!(validate_parse_jitter_ms("1").is_ok());
        assert!(validate_parse_jitter_ms("1000").is_ok());
        assert!(validate_parse_jitter_ms("0.5").is_err());
        assert!(validate_parse_jitter_ms("1001").is_err());
        assert!(validate_parse_jitter_ms("not_a_number").is_err());
    }
}
