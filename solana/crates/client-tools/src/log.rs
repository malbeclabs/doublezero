#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! log_info {
    ($($arg:tt)*) => {
        ::tracing::info!($($arg)*);
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
        ::tracing::error!($($arg)*);
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
        ::tracing::warn!($($arg)*);
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
        ::tracing::debug!($($arg)*);
    };
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! log_debug {
    ($($arg:tt)*) => {
        println!($($arg)*);
    };
}
