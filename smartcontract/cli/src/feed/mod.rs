pub mod create;
pub mod delete;
pub mod get;
pub mod list;
pub mod update;

use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

/// Parse a `--metro` argument of the form `EXCHANGE_PK=GROUP_PK[,GROUP_PK...]` into
/// `(exchange_pk, [group_pk, ...])`. An empty group list (`EXCHANGE_PK=`) is allowed.
pub fn parse_metro(s: &str) -> Result<(Pubkey, Vec<Pubkey>), String> {
    let (exchange, groups) = s
        .split_once('=')
        .ok_or_else(|| format!("expected EXCHANGE_PK=GROUP_PK[,GROUP_PK...], got '{s}'"))?;
    let exchange_pk =
        Pubkey::from_str(exchange.trim()).map_err(|e| format!("invalid exchange pubkey: {e}"))?;
    let group_pks = groups
        .split(',')
        .map(str::trim)
        .filter(|g| !g.is_empty())
        .map(|g| Pubkey::from_str(g).map_err(|e| format!("invalid group pubkey '{g}': {e}")))
        .collect::<Result<Vec<_>, _>>()?;
    Ok((exchange_pk, group_pks))
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
        let (e, gs) = parse_metro(&format!("{ex}={g1},{g2}")).unwrap();
        assert_eq!(e, ex);
        assert_eq!(gs, vec![g1, g2]);

        // No groups is allowed.
        let (e, gs) = parse_metro(&format!("{ex}=")).unwrap();
        assert_eq!(e, ex);
        assert!(gs.is_empty());

        assert!(parse_metro("not-a-pair").is_err());
        assert!(parse_metro("bad=alsoBad").is_err());
    }
}
