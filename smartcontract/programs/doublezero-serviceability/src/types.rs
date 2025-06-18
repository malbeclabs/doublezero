use ipnetwork::Ipv4Network;
use std::{net::Ipv4Addr, str::FromStr};

pub type IpV4 = [u8; 4]; // 4
pub type NetworkV4 = (IpV4, u8); // 5
pub type NetworkV4List = Vec<NetworkV4>; // 4 + len * 5

pub fn ipv4_parse(str: &str) -> Result<IpV4, String> {
    Ipv4Addr::from_str(str)
        .map(|ip| ip.octets())
        .map_err(|_| String::from("Invalid IPv4 address format"))
}

pub fn ipv4_to_string(ip: &IpV4) -> String {
    format!("{}.{}.{}.{}", ip[0], ip[1], ip[2], ip[3])
}

pub fn networkv4_parse(str: &str) -> Result<NetworkV4, String> {
    match Ipv4Network::from_str(str) {
        Ok(net) => {
            let ip = net.ip().octets();
            let prefix = net.prefix();
            Ok((ip, prefix))
        }
        Err(_) => Err(String::from("Invalid network format")),
    }
}

/* 1.2.3.4/5, */
pub fn networkv4_list_parse(input: &str) -> Result<NetworkV4List, String> {
    let mut collected = Vec::new();

    // remove spaces
    let input = &input.replace(" ", "");

    for val in input.split(',') {
        let val = networkv4_parse(val)?;
        collected.push(val);
    }

    Ok(collected)
}

pub fn networkv4_to_string(net: &NetworkV4) -> String {
    format!(
        "{}.{}.{}.{}/{}",
        net.0[0], net.0[1], net.0[2], net.0[3], net.1
    )
}

pub fn networkv4_list_to_string(net_list: &NetworkV4List) -> String {
    net_list
        .iter()
        .map(networkv4_to_string)
        .collect::<Vec<String>>()
        .join(", ")
}

pub fn networkv4_to_ipnetwork(net: &NetworkV4) -> Ipv4Network {
    let ip_addr = Ipv4Addr::new(net.0[0], net.0[1], net.0[2], net.0[3]);
    Ipv4Network::new(ip_addr, net.1).expect("Invalid network")
}

pub fn bandwidth_parse(str: &str) -> Result<u64, String> {
    let str = str.to_lowercase().replace(" ", "");
    let str = str.replace("gbps", "g");
    let str = str.replace("mbps", "m");
    let str = str.replace("kbps", "k");
    let str = str.replace("bps", "b");
    let mut unit = str.chars().last().unwrap_or('k');

    if !unit.is_alphabetic() {
        unit = 'k';
    }

    let str: String = str.chars().filter(|c| !c.is_alphabetic()).collect();

    let val = str
        .parse::<f64>()
        .map_err(|_| String::from("Invalid bandwidth value"))?;

    match unit {
        'b' => Ok(val as u64),
        'k' => Ok((val * 1000.0) as u64),
        'm' => Ok((val * 1000000.0) as u64),
        'g' => Ok((val * 1000000000.0) as u64),
        _ => Ok((val * 1000.0) as u64),
    }
}

pub fn bandwidth_to_string(bandwidth: &u64) -> String {
    if *bandwidth < 1000 {
        format!("{bandwidth}bps")
    } else if *bandwidth < 1000000 {
        if bandwidth % 1000 == 0 {
            format!("{}Kbps", bandwidth / 1000)
        } else {
            format!("{:.2}Kbps", *bandwidth as f64 / 1000.0)
        }
    } else if *bandwidth < 1000000000 {
        if bandwidth % 1000000 == 0 {
            format!("{}Mbps", bandwidth / 1000000)
        } else {
            format!("{:.2}Mbps", *bandwidth as f64 / 1000000.0)
        }
    } else if bandwidth % 1000000000 == 0 {
        format!("{}Gbps", bandwidth / 1000000000)
    } else {
        format!("{:.2}Gbps", *bandwidth as f64 / 1000000000.0)
    }
}

pub fn delay_to_string(delay_ns: u64) -> String {
    let delay_ms = delay_ns as f64 / 1_000_000.0;
    format!("{delay_ms:.2}ms")
}

pub fn jitter_to_string(delay_ns: u64) -> String {
    let delay_ms = delay_ns as f64 / 1_000_000.0;
    format!("{delay_ms:.2}ms")
}

pub fn ipv4_is_bogon(ip: IpV4) -> bool {
    let bogons = vec![
        Ipv4Network::new("0.0.0.0".parse().unwrap(), 8).unwrap(),
        Ipv4Network::new("10.0.0.0".parse().unwrap(), 8).unwrap(),
        Ipv4Network::new("100.64.0.0".parse().unwrap(), 10).unwrap(),
        Ipv4Network::new("127.0.0.0".parse().unwrap(), 8).unwrap(),
        Ipv4Network::new("169.254.0.0".parse().unwrap(), 16).unwrap(),
        Ipv4Network::new("172.16.0.0".parse().unwrap(), 12).unwrap(),
        Ipv4Network::new("192.0.2.0".parse().unwrap(), 24).unwrap(),
        Ipv4Network::new("192.168.0.0".parse().unwrap(), 16).unwrap(),
        Ipv4Network::new("198.18.0.0".parse().unwrap(), 15).unwrap(),
        Ipv4Network::new("224.0.0.0".parse().unwrap(), 4).unwrap(),
        Ipv4Network::new("240.0.0.0".parse().unwrap(), 4).unwrap(),
    ];

    let ip = Ipv4Addr::new(ip[0], ip[1], ip[2], ip[3]);
    for net in bogons {
        if net.contains(ip) {
            return true;
        }
    }
    false
}

pub fn ipv4_is_rfc1918(ip: IpV4) -> bool {
    let private_ranges = vec![
        Ipv4Network::new("10.0.0.0".parse().unwrap(), 8).unwrap(),
        Ipv4Network::new("172.16.0.0".parse().unwrap(), 12).unwrap(),
        Ipv4Network::new("192.168.0.0".parse().unwrap(), 16).unwrap(),
    ];

    let ip = Ipv4Addr::new(ip[0], ip[1], ip[2], ip[3]);
    for net in private_ranges {
        if net.contains(ip) {
            return true;
        }
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ipv4_parse() {
        let ip = ipv4_parse("1.2.3.4");
        assert!(ip.is_ok());
        assert_eq!(ip.unwrap(), [1, 2, 3, 4]);
    }

    #[test]
    fn test_ipv4_serialize() {
        let val = ipv4_parse("1.2.3.4");
        let data = borsh::to_vec(&val.clone().unwrap()).unwrap();
        let val2 = borsh::from_slice::<IpV4>(&data).unwrap();
        assert!(val.is_ok());
        assert_eq!(val.unwrap(), val2);
        assert_eq!(data.len(), 4);
    }

    #[test]
    fn test_networkv4_parse() {
        let ip = networkv4_parse("10.0.0.1/24");
        assert!(ip.is_ok());
        assert_eq!(ip.unwrap(), ([10, 0, 0, 1], 24));
    }

    #[test]
    fn test_networkv4_serialize() {
        let val = networkv4_parse("10.0.0.1/24");
        let data = borsh::to_vec(&val.clone().unwrap()).unwrap();
        let val2 = borsh::from_slice::<NetworkV4>(&data).unwrap();
        assert!(val.is_ok());
        assert_eq!(val.unwrap(), val2);
        assert_eq!(data.len(), 5);
    }

    #[test]
    fn test_networkv4_list_parse() {
        let ip = networkv4_list_parse("10.0.0.1/24,11.0.0.1/24");
        assert!(ip.is_ok());
        assert_eq!(ip.unwrap(), vec!(([10, 0, 0, 1], 24), ([11, 0, 0, 1], 24)));
    }

    #[test]
    fn test_networkv4_list_serialize() {
        let val = networkv4_list_parse("10.0.0.1/24,11.0.0.1/24");
        let data = borsh::to_vec(&val.clone().unwrap()).unwrap();
        let val2 = borsh::from_slice::<NetworkV4List>(&data).unwrap();
        assert!(val.is_ok());
        assert_eq!(val.unwrap(), val2);
        assert_eq!(data.len(), 4 + 5 + 5);
    }

    #[test]
    fn test_bandwidth_parse() {
        assert_eq!(bandwidth_parse("500").unwrap(), 500000, "500");
        assert_eq!(bandwidth_parse("500bps").unwrap(), 500, "500bps");
        assert_eq!(bandwidth_parse("1.50Kbps").unwrap(), 1500, "1.50Kbps");
        assert_eq!(bandwidth_parse("2.50Mbps").unwrap(), 2500000, "2.50Mbps");
        assert_eq!(bandwidth_parse("3.50Gbps").unwrap(), 3500000000, "3.50Gbps");
    }

    #[test]
    fn test_bandwidth_to_string() {
        assert_eq!(bandwidth_to_string(&500), "500bps", "500");
        assert_eq!(bandwidth_to_string(&1500), "1.50Kbps", "1.50Kbps");
        assert_eq!(bandwidth_to_string(&2500000), "2.50Mbps", "2.50Mbps");
        assert_eq!(bandwidth_to_string(&3500000000), "3.50Gbps", "3.50Gbps");
    }

    #[test]
    fn test_delay_to_string() {
        assert_eq!(delay_to_string(1_000_000), "1.00ms");
        assert_eq!(delay_to_string(1_500_000), "1.50ms");
        assert_eq!(delay_to_string(1_000_000_000), "1000.00ms");
    }

    #[test]
    fn test_jitter_to_string() {
        assert_eq!(jitter_to_string(1_000_000), "1.00ms");
        assert_eq!(jitter_to_string(1_500_000), "1.50ms");
        assert_eq!(jitter_to_string(1_000_000_000), "1000.00ms");
    }
}
