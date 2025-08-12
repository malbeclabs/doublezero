use doublezero_program_common::normalize_account_code;
use doublezero_sdk::bandwidth_parse;
use regex::Regex;
use solana_sdk::pubkey::Pubkey;
use std::sync::LazyLock;

pub fn validate_code(val: &str) -> Result<String, String> {
    normalize_account_code(val).map_err(String::from)
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

static INTERFACE_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)^(Ethernet\d+(/\d+)?|Switch\d+/\d+/\d+|Loopback\d+|Port-Channel\d+|Vlan\d+)(\.\d+)?$",
    )
    .unwrap()
});

static INTERFACE_SHORTHAND_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)^(et\d+(/\d+)?|sw\d+/\d+/\d+|lo\d+|po\d+|vl\d+)(\.\d*)?$").unwrap()
});

fn capitalize(s: String) -> String {
    let ls = s.to_lowercase();
    let mut c = ls.chars();
    match c.next() {
        None => String::new(),
        Some(f) => f.to_uppercase().collect::<String>() + c.as_str(),
    }
}

pub fn validate_iface(val: &str) -> Result<String, String> {
    if INTERFACE_REGEX.is_match(val) {
        Ok(capitalize(val.to_string()))
    } else if INTERFACE_SHORTHAND_REGEX.is_match(val) {
        match val[0..2].to_lowercase().as_str() {
            "et" => Ok(format!("Ethernet{}", &val[2..])),
            "sw" => Ok(format!("Switch{}", &val[2..])),
            "lo" => Ok(format!("Loopback{}", &val[2..])),
            "po" => Ok(format!("Port-Channel{}", &val[2..])),
            "vl" => Ok(format!("Vlan{}", &val[2..])),
            _ => Err(String::from("Invalid interface shorthand")),
        }
    } else {
        Err(String::from(
            "Interface name not valid. Must match: EthernetX[/X], SwitchX/X/X, LoopbackX, Port-ChannelX, or VlanX",
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_validate_code() {
        assert!(validate_code("abc_123-XYZ").is_ok());
        assert!(validate_code("abc@123").is_err());
    }

    #[test]
    fn test_validate_and_normalize_code() {
        let expected_valid = "abc_123-XYZ".to_string();
        let result = validate_code("abc 123-XYZ");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), expected_valid);
        assert!(validate_code("abc_123-:XYZ").is_err());
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

    #[test]
    fn test_validate_iface() {
        assert!(validate_iface("Ethernet1").is_ok());
        assert!(validate_iface("Ethernet1/1").is_ok());
        assert!(validate_iface("ethernet2/2").unwrap() == "Ethernet2/2");
        assert!(validate_iface("ETHERNET2/2").unwrap() == "Ethernet2/2");
        assert!(validate_iface("Ethernet1/1.123").is_ok());
        assert!(validate_iface("Ethernet1/1.abc").is_err());
        assert!(validate_iface("et2/4").unwrap() == "Ethernet2/4");
        assert!(validate_iface("Switch1/1/1").is_ok());
        assert!(validate_iface("Switch1/1/1.42").is_ok());
        assert!(validate_iface("Switch1/1/1.foobar").is_err());
        assert!(validate_iface("sw3/12/20").unwrap() == "Switch3/12/20");
        assert!(validate_iface("Loopback0").is_ok());
        assert!(validate_iface("Port-Channel1").is_ok());
        assert!(validate_iface("Port-Channel1.5000").is_ok());
        assert!(validate_iface("Port-Channel1.").is_err());
        assert!(validate_iface("Vlan123").is_ok());
        assert!(validate_iface("Vlan123.456").is_ok());
        assert!(validate_iface("vl1001").unwrap() == "Vlan1001");
        assert!(validate_iface("InvalidInterface").is_err());
    }
}
