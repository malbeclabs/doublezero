// Re-export tracing so dependent crates don't need to depend on it directly
#[cfg(feature = "tracing")]
pub use tracing;

#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! log_info {
    ($($arg:tt)*) => {
        $crate::log::tracing::info!($($arg)*);
    };
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! log_info {
    ($($arg:tt)*) => {
        println!($($arg)*);
    };
}

#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! log_error {
    ($($arg:tt)*) => {
        $crate::log::tracing::error!($($arg)*);
    };
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! log_error {
    ($($arg:tt)*) => {
        eprintln!($($arg)*);
    };
}

#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! log_warn {
    ($($arg:tt)*) => {
        $crate::log::tracing::warn!($($arg)*);
    };
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! log_warn {
    ($($arg:tt)*) => {
        eprintln!($($arg)*);
    };
}

#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! log_debug {
    ($($arg:tt)*) => {
        $crate::log::tracing::debug!($($arg)*);
    };
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! log_debug {
    ($($arg:tt)*) => {
        println!($($arg)*);
    };
}
