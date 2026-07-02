pub mod create;
pub mod delete;
pub mod get;
pub mod list;
pub mod update;

use doublezero_serviceability::state::feed::MetroGroups;
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

/// Parse a `--metro` argument of the form `EXCHANGE_PK=GROUP_PK[,GROUP_PK...]` into a
/// [`MetroGroups`]. An empty group list (`EXCHANGE_PK=`) is allowed.
pub fn parse_metro(s: &str) -> Result<MetroGroups, String> {
    let (exchange, groups) = s
        .split_once('=')
        .ok_or_else(|| format!("expected EXCHANGE_PK=GROUP_PK[,GROUP_PK...], got '{s}'"))?;
    let exchange =
        Pubkey::from_str(exchange.trim()).map_err(|e| format!("invalid exchange pubkey: {e}"))?;
    let groups = groups
        .split(',')
        .map(str::trim)
        .filter(|g| !g.is_empty())
        .map(|g| Pubkey::from_str(g).map_err(|e| format!("invalid group pubkey '{g}': {e}")))
        .collect::<Result<Vec<_>, _>>()?;
    Ok(MetroGroups { exchange, groups })
}

#[cfg(test)]
mod tests {
    use super::parse_metro;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_parse_metro() {
        let ex = Pubkey::new_unique();
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let m = parse_metro(&format!("{ex}={g1},{g2}")).unwrap();
        assert_eq!(m.exchange, ex);
        assert_eq!(m.groups, vec![g1, g2]);

        // No groups is allowed.
        let m = parse_metro(&format!("{ex}=")).unwrap();
        assert_eq!(m.exchange, ex);
        assert!(m.groups.is_empty());

        assert!(parse_metro("not-a-pair").is_err());
        assert!(parse_metro("bad=alsoBad").is_err());
    }
}
