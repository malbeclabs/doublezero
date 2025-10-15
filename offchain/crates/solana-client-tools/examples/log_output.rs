use doublezero_solana_client_tools::{log_debug, log_error, log_info, log_warn};

fn main() {
    #[cfg(feature = "tracing")]
    {
        use tracing_subscriber::FmtSubscriber;

        let subscriber = FmtSubscriber::builder()
            .with_max_level(tracing::Level::DEBUG)
            .finish();

        tracing::subscriber::set_global_default(subscriber).unwrap();

        println!("Running with `tracing` feature...");
    }
    #[cfg(not(feature = "tracing"))]
    {
        println!("Running without `tracing` feature...");
    }

    // Test all logging levels
    log_info!("Testing `log_info` macro");
    log_error!("Testing `log_error` macro");
    log_warn!("Testing `log_warn` macro");
    log_debug!("Testing `log_debug` macro");

    // Test with formatting
    let value = 42;
    let name = "test";
    log_info!("Info with formatting: {} {}", name, value);
    log_error!("Error with value: {value}");
    log_warn!("Warning: {} items remaining", value);
    log_debug!("Debug: name={name}, value={value}");

    // Empty logs
    log_info!("");

    // Complex formatting
    let list = vec![1, 2, 3];
    log_info!("Processing list: {:?}", list);
    log_error!("Failed to process item at index {}", 0);
    log_warn!("Performance warning: {} ms", 150);
}
