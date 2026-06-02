use anyhow::{Result, bail};

pub fn parse_sol_amount_to_lamports(sol_amount_str: String) -> Result<u64> {
    let sol_amount_str = sol_amount_str.trim();

    if sol_amount_str.is_empty() {
        bail!("SOL amount cannot be empty");
    }

    let sol_amount = sol_amount_str
        .parse::<f64>()
        .map_err(|_| anyhow::anyhow!("Invalid SOL amount: '{sol_amount_str}'"))?;

    if sol_amount <= 0.0 {
        bail!("SOL amount must be a positive value");
    }

    if sol_amount > (u64::MAX as f64 / 1e9) {
        bail!("SOL amount too large");
    }

    // Check that value is at most 9 decimal places.
    if let Some(decimal_index) = sol_amount_str.find('.') {
        let decimal_places = sol_amount_str.len() - decimal_index - 1;
        if decimal_places > 9 {
            bail!("SOL amount cannot have more than 9 decimal places");
        }
    }

    Ok((sol_amount * 1e9).round() as u64)
}
