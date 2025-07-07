//! Test data loading

use anyhow::{Context, Result, bail};
use duckdb::Connection;
use rust_decimal::{Decimal, prelude::*};
use std::str::FromStr;

/// Represents a row from simulated_public_links.csv
#[derive(Debug)]
pub struct SimulatedPublicLink {
    pub start: String,
    pub end: String,
    pub cost: Decimal,
}

/// Represents a row from simulated_private_links.csv
#[derive(Debug)]
pub struct SimulatedPrivateLink {
    pub start: String,
    pub end: String,
    pub cost: Decimal,
    pub bandwidth: Decimal,
    pub operator1: String,
    pub operator2: String,
    pub uptime: Decimal,
    pub shared: usize,
}

/// Represents a row from simulated_demand.csv
#[derive(Debug)]
pub struct SimulatedDemand {
    pub start: String,
    pub end: String,
    pub traffic: Decimal,
    pub demand_type: usize,
}

/// Load simulated test data into DuckDB
pub fn load_simulated_data(conn: &Connection) -> Result<()> {
    // Create tables for simulated data
    create_simulated_tables(conn)?;
    Ok(())
}

/// Create tables for simulated test data
fn create_simulated_tables(conn: &Connection) -> Result<()> {
    // Create public links table
    conn.execute(
        r#"
        CREATE TABLE IF NOT EXISTS simulated_public_links (
            "start" VARCHAR,
            "end" VARCHAR,
            cost DECIMAL(38, 18)
        )
        "#,
        [],
    )
    .context("Failed to create simulated_public_links table")?;

    // Create private links table
    conn.execute(
        r#"
        CREATE TABLE IF NOT EXISTS simulated_private_links (
            "start" VARCHAR,
            "end" VARCHAR,
            cost DECIMAL(38, 18),
            bandwidth DECIMAL(38, 18),
            operator1 VARCHAR,
            operator2 VARCHAR,
            uptime DECIMAL(38, 18),
            shared INTEGER
        )
        "#,
        [],
    )
    .context("Failed to create simulated_private_links table")?;

    // Create demand table
    conn.execute(
        r#"
        CREATE TABLE IF NOT EXISTS simulated_demand (
            "start" VARCHAR,
            "end" VARCHAR,
            traffic DECIMAL(38, 18),
            type INTEGER
        )
        "#,
        [],
    )
    .context("Failed to create simulated_demand table")?;

    Ok(())
}

/// Load public links from CSV data
pub fn load_public_links_csv(conn: &Connection, csv_data: &str) -> Result<()> {
    // First, clear existing data
    conn.execute("DELETE FROM simulated_public_links", [])?;

    let mut reader = csv::Reader::from_reader(csv_data.as_bytes());

    for (i, result) in reader.records().enumerate() {
        let record = result.context(format!("Failed to parse CSV record at line {}", i + 2))?;

        if record.len() < 3 {
            bail!(
                "Invalid CSV format at line {}: expected at least 3 columns, got {}",
                i + 2,
                record.len()
            );
        }

        let start = record.get(0).unwrap();
        let end = record.get(1).unwrap();
        let cost_str = record.get(2).unwrap();

        let cost = Decimal::from_str(cost_str).context(format!(
            "Failed to parse cost '{}' at line {}",
            cost_str,
            i + 2
        ))?;

        conn.execute(
            r#"INSERT INTO simulated_public_links ("start", "end", cost) VALUES (?, ?, ?)"#,
            duckdb::params![start, end, cost.to_string()],
        )?;
    }

    Ok(())
}

/// Load private links from CSV data
pub fn load_private_links_csv(conn: &Connection, csv_data: &str) -> Result<()> {
    // First, clear existing data
    conn.execute("DELETE FROM simulated_private_links", [])?;

    let mut reader = csv::Reader::from_reader(csv_data.as_bytes());

    for (i, result) in reader.records().enumerate() {
        let record = result.context(format!("Failed to parse CSV record at line {}", i + 2))?;

        if record.len() < 8 {
            anyhow::bail!(
                "Invalid CSV format at line {}: expected at least 8 columns, got {}",
                i + 2,
                record.len()
            );
        }

        let start = record.get(0).unwrap();
        let end = record.get(1).unwrap();
        let cost = Decimal::from_str(record.get(2).unwrap())
            .context(format!("Failed to parse cost at line {}", i + 2))?;
        let bandwidth = Decimal::from_str(record.get(3).unwrap())
            .context(format!("Failed to parse bandwidth at line {}", i + 2))?;
        let operator1 = record.get(4).unwrap();
        let operator2 = record.get(5).unwrap();
        let uptime = Decimal::from_str(record.get(6).unwrap())
            .context(format!("Failed to parse uptime at line {}", i + 2))?;

        let shared_str = record.get(7).unwrap();
        let shared = if shared_str == "NA" {
            0
        } else {
            shared_str.parse::<usize>().context(format!(
                "Failed to parse shared value '{}' at line {}",
                shared_str,
                i + 2
            ))?
        };

        conn.execute(
            r#"INSERT INTO simulated_private_links 
               ("start", "end", cost, bandwidth, operator1, operator2, uptime, shared) 
               VALUES (?, ?, ?, ?, ?, ?, ?, ?)"#,
            duckdb::params![
                start,
                end,
                cost.to_string(),
                bandwidth.to_string(),
                operator1,
                operator2,
                uptime.to_string(),
                shared
            ],
        )?;
    }

    Ok(())
}

/// Load demand data from CSV
pub fn load_demand_csv(conn: &Connection, csv_data: &str) -> Result<()> {
    // First, clear existing data
    conn.execute("DELETE FROM simulated_demand", [])?;

    let mut reader = csv::Reader::from_reader(csv_data.as_bytes());

    for (i, result) in reader.records().enumerate() {
        let record = result.context(format!("Failed to parse CSV record at line {}", i + 2))?;

        if record.len() < 4 {
            anyhow::bail!(
                "Invalid CSV format at line {}: expected at least 4 columns, got {}",
                i + 2,
                record.len()
            );
        }

        let start = record.get(0).unwrap();
        let end = record.get(1).unwrap();
        let traffic = Decimal::from_str(record.get(2).unwrap())
            .context(format!("Failed to parse traffic at line {}", i + 2))?;
        let demand_type = record
            .get(3)
            .unwrap()
            .parse::<usize>()
            .context(format!("Failed to parse demand type at line {}", i + 2))?;

        conn.execute(
            r#"INSERT INTO simulated_demand ("start", "end", traffic, type) VALUES (?, ?, ?, ?)"#,
            duckdb::params![start, end, traffic.to_string(), demand_type],
        )?;
    }

    Ok(())
}

/// Get all public links from simulated data
pub fn get_simulated_public_links(conn: &Connection) -> Result<Vec<SimulatedPublicLink>> {
    let mut stmt = conn.prepare(r#"SELECT "start", "end", cost FROM simulated_public_links"#)?;

    let links = stmt
        .query_map([], |row| {
            let cost_f64: f64 = row.get(2)?;
            let cost = Decimal::from_f64(cost_f64).ok_or_else(|| {
                duckdb::Error::FromSqlConversionFailure(
                    2,
                    duckdb::types::Type::Double,
                    Box::new(std::fmt::Error),
                )
            })?;
            Ok(SimulatedPublicLink {
                start: row.get(0)?,
                end: row.get(1)?,
                cost,
            })
        })?
        .collect::<std::result::Result<Vec<_>, _>>()?;

    Ok(links)
}

/// Get all private links from simulated data
pub fn get_simulated_private_links(conn: &Connection) -> Result<Vec<SimulatedPrivateLink>> {
    let mut stmt = conn.prepare(
        r#"SELECT "start", "end", cost, bandwidth, operator1, operator2, uptime, shared 
         FROM simulated_private_links"#,
    )?;

    let links = stmt
        .query_map([], |row| {
            let cost_f64: f64 = row.get(2)?;
            let cost = Decimal::from_f64(cost_f64).ok_or_else(|| {
                duckdb::Error::FromSqlConversionFailure(
                    2,
                    duckdb::types::Type::Double,
                    Box::new(std::fmt::Error),
                )
            })?;

            let bandwidth_f64: f64 = row.get(3)?;
            let bandwidth = Decimal::from_f64(bandwidth_f64).ok_or_else(|| {
                duckdb::Error::FromSqlConversionFailure(
                    3,
                    duckdb::types::Type::Double,
                    Box::new(std::fmt::Error),
                )
            })?;

            let uptime_f64: f64 = row.get(6)?;
            let uptime = Decimal::from_f64(uptime_f64).ok_or_else(|| {
                duckdb::Error::FromSqlConversionFailure(
                    6,
                    duckdb::types::Type::Double,
                    Box::new(std::fmt::Error),
                )
            })?;

            Ok(SimulatedPrivateLink {
                start: row.get(0)?,
                end: row.get(1)?,
                cost,
                bandwidth,
                operator1: row.get(4)?,
                operator2: row.get(5)?,
                uptime,
                shared: row.get(7)?,
            })
        })?
        .collect::<std::result::Result<Vec<_>, _>>()?;

    Ok(links)
}

/// Get all demand from simulated data
pub fn get_simulated_demand(conn: &Connection) -> Result<Vec<SimulatedDemand>> {
    let mut stmt = conn.prepare(r#"SELECT "start", "end", traffic, type FROM simulated_demand"#)?;

    let demands = stmt
        .query_map([], |row| {
            let traffic_f64: f64 = row.get(2)?;
            let traffic = Decimal::from_f64(traffic_f64).ok_or_else(|| {
                duckdb::Error::FromSqlConversionFailure(
                    2,
                    duckdb::types::Type::Double,
                    Box::new(std::fmt::Error),
                )
            })?;

            Ok(SimulatedDemand {
                start: row.get(0)?,
                end: row.get(1)?,
                traffic,
                demand_type: row.get(3)?,
            })
        })?
        .collect::<std::result::Result<Vec<_>, _>>()?;

    Ok(demands)
}

#[cfg(test)]
mod tests {
    use super::*;
    use duckdb::Connection;

    #[test]
    fn test_create_tables() {
        let conn = Connection::open_in_memory().unwrap();
        create_simulated_tables(&conn).unwrap();

        // Verify tables exist
        let count: i32 = conn
            .query_row(
                "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'simulated_public_links'",
                [],
                |row| row.get(0),
            )
            .unwrap();
        assert_eq!(count, 1);
    }

    #[test]
    fn test_load_public_links_csv() {
        let conn = Connection::open_in_memory().unwrap();
        create_simulated_tables(&conn).unwrap();

        let csv_data = r#"Start,End,Cost
NYC3,WAS3,0.5
WAS3,LON3,2.5
LON3,FRA3,0.8"#;

        load_public_links_csv(&conn, csv_data).unwrap();

        let links = get_simulated_public_links(&conn).unwrap();
        assert_eq!(links.len(), 3);
        assert_eq!(links[0].start, "NYC3");
        assert_eq!(links[0].end, "WAS3");
        assert_eq!(links[0].cost, Decimal::from_str("0.5").unwrap());
    }
}
