// Description: Validates and standardizes network interface names.
// Does not use regex because of use on-chain.

fn capitalize(s: String) -> String {
    let ls = s.to_lowercase();
    let mut c = ls.chars();
    match c.next() {
        None => String::new(),
        Some(f) => f.to_uppercase().collect::<String>() + c.as_str(),
    }
}

pub fn validate_iface(val: &str) -> Result<String, String> {
    if is_valid_interface_name(val) {
        return Ok(capitalize(val.to_string()));
    } else {
        let alt = match val[0..2].to_lowercase().as_str() {
            "et" => format!("Ethernet{}", &val[2..]),
            "sw" => format!("Switch{}", &val[2..]),
            "lo" => format!("Loopback{}", &val[2..]),
            "po" => format!("Port-channel{}", &val[2..]),
            "vl" => format!("Vlan{}", &val[2..]),
            _ => return Err(String::from("Invalid interface shorthand")),
        };
        if is_valid_interface_name(&alt) {
            return Ok(capitalize(alt));
        }
    }

    Err(String::from(
        "Interface name not valid. Must match: EthernetX[/X], SwitchX/X/X, LoopbackX, Port-channelX, or VlanX",
    ))
}

fn is_valid_interface_name(s: &str) -> bool {
    let s_lower = s.to_ascii_lowercase();

    // Split the string into the main interface name and an optional subinterface part
    let parts: Vec<&str> = s_lower.split('.').collect();
    let main_interface_part = parts[0];
    let subinterface_part = if parts.len() > 1 {
        Some(parts[1])
    } else {
        None
    };

    // Check the main interface part
    let main_interface_match = if main_interface_part.starts_with("ethernet") {
        is_ethernet(main_interface_part)
    } else if main_interface_part.starts_with("switch") {
        is_switch(main_interface_part)
    } else if main_interface_part.starts_with("loopback") {
        is_loopback(main_interface_part)
    } else if main_interface_part.starts_with("port-channel") {
        is_port_channel(main_interface_part)
    } else if main_interface_part.starts_with("vlan") {
        is_vlan(main_interface_part)
    } else {
        false
    };

    if !main_interface_match {
        return false;
    }

    // Check the optional subinterface part
    if let Some(sub) = subinterface_part {
        is_valid_subinterface(sub)
    } else {
        // If there is no subinterface part, the match is valid
        true
    }
}

// Checks for "Ethernet\d+(/\d+)?"
fn is_ethernet(s: &str) -> bool {
    let s_without_prefix = s.strip_prefix("ethernet").unwrap_or("");
    let parts: Vec<&str> = s_without_prefix.split('/').collect();

    // Must be "Ethernet\d+" or "Ethernet\d+/\d+"
    if parts.is_empty() || parts.len() > 2 {
        return false;
    }

    // Check if both parts are valid numbers and the full string is consumed
    for part in parts {
        if part.is_empty() || part.parse::<u32>().is_err() {
            return false;
        }
    }

    true
}

// Checks for "Switch\d+/\d+/\d+"
fn is_switch(s: &str) -> bool {
    let s_without_prefix = s.strip_prefix("switch").unwrap_or("");
    let parts: Vec<&str> = s_without_prefix.split('/').collect();

    // Must be "Switch\d+/\d+/\d+"
    if parts.len() != 3 {
        return false;
    }

    for part in parts {
        if part.parse::<u32>().is_err() {
            return false;
        }
    }
    true
}

// Checks for "Loopback\d+"
fn is_loopback(s: &str) -> bool {
    let s_without_prefix = s.strip_prefix("loopback").unwrap_or("");
    s_without_prefix.parse::<u32>().is_ok()
}

// Checks for "Port-channel\d+"
fn is_port_channel(s: &str) -> bool {
    let s_without_prefix = s.strip_prefix("port-channel").unwrap_or("");
    s_without_prefix.parse::<u32>().is_ok()
}

// Checks for "Vlan\d+"
fn is_vlan(s: &str) -> bool {
    let s_without_prefix = s.strip_prefix("vlan").unwrap_or("");
    s_without_prefix.parse::<u32>().is_ok()
}

// Checks for the optional ".\d+" subinterface part
fn is_valid_subinterface(s: &str) -> bool {
    !s.is_empty() && s.parse::<u32>().is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;

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
        assert!(validate_iface("Port-Channel1").unwrap() == "Port-channel1");
        assert!(validate_iface("Port-Channel1.5000").is_ok());
        assert!(validate_iface("Port-Channel1.").is_err());
        assert!(validate_iface("po1000.2035").unwrap() == "Port-channel1000.2035");
        assert!(validate_iface("Vlan123").is_ok());
        assert!(validate_iface("Vlan123.456").is_ok());
        assert!(validate_iface("vl1001").unwrap() == "Vlan1001");
        assert!(validate_iface("InvalidInterface").is_err());
    }
}
