use network_shapley::types::{Demand, PrivateLink};
use tabled::{builder::Builder as TableBuilder, settings::Style};

pub fn print_private_links(private_links: &[PrivateLink]) -> String {
    let mut printable = vec![vec![
        "device1".to_string(),
        "device2".to_string(),
        "latency(ms)".to_string(),
        "bandwidth(Gbps)".to_string(),
        "uptime".to_string(),
        "shared".to_string(),
    ]];

    for pl in private_links {
        let row = vec![
            pl.device1.to_string(),
            pl.device2.to_string(),
            pl.latency.to_string(),
            pl.bandwidth.to_string(),
            pl.uptime.to_string(),
            format!("{:?}", pl.shared),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}

pub fn print_demands(demands: &[Demand], k: usize) -> String {
    let mut printable = vec![vec![
        "start".to_string(),
        "end".to_string(),
        "receivers".to_string(),
        "traffic".to_string(),
        "priority".to_string(),
        "type".to_string(),
        "multicast".to_string(),
    ]];

    for demand in demands.iter().take(k) {
        let row = vec![
            demand.start.to_string(),
            demand.end.to_string(),
            demand.receivers.to_string(),
            demand.traffic.to_string(),
            demand.priority.to_string(),
            demand.kind.to_string(),
            demand.multicast.to_string(),
        ];
        printable.push(row);
    }

    TableBuilder::from(printable)
        .build()
        .with(Style::psql().remove_horizontals())
        .to_string()
}
