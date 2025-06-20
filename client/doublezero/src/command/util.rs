use eyre::Result;
use tabled::{settings::Style, Table, Tabled};

pub fn show_output<T>(data: Vec<T>, is_output_json: bool) -> Result<()>
where
    T: serde::Serialize + Tabled,
{
    let output = if is_output_json {
        serde_json::to_string_pretty(&data)?
    } else {
        Table::new(data)
            .with(Style::psql().remove_horizontals())
            .to_string()
    };
    println!("{output}");
    Ok(())
}
