pub fn bandwidth_parse(str: &str) -> Result<u64, String> {
    let str = str.to_lowercase().replace(" ", "");
    let str = str.replace("gbps", "g");
    let str = str.replace("mbps", "m");
    let str = str.replace("kbps", "k");
    let str = str.replace("bps", "b");
    let unit = str.chars().last().unwrap_or('\0');

    if !unit.is_alphabetic() {
        return Err(String::from(
            "bandwidth requires a unit (e.g. 100Kbps, 1Mbps, 10Gbps)",
        ));
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
    match *bandwidth {
        0..=999 => {
            format!("{bandwidth}bps")
        }
        1_000..=999_999 => {
            if bandwidth % 1000 == 0 {
                format!("{}Kbps", bandwidth / 1000)
            } else {
                format!("{:.2}Kbps", *bandwidth as f64 / 1000.0)
            }
        }
        1_000_000..=999_999_999 => {
            if bandwidth % 1000000 == 0 {
                format!("{}Mbps", bandwidth / 1000000)
            } else {
                format!("{:.2}Mbps", *bandwidth as f64 / 1000000.0)
            }
        }
        1_000_000_000.. => {
            format!("{}Gbps", bandwidth / 1000000000)
        }
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
