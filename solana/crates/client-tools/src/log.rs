#[macro_export]
macro_rules! log_info {
    ($($arg:tt)*) => {
        #[cfg(feature = "tracing")]
        {
            ::tracing::info!($($arg)*);
        }
        #[cfg(not(feature = "tracing"))]
        {
            println!($($arg)*);
        }
    };
}

#[macro_export]
macro_rules! log_error {
    ($($arg:tt)*) => {
        #[cfg(feature = "tracing")]
        {
            ::tracing::error!($($arg)*);
        }
        #[cfg(not(feature = "tracing"))]
        {
            eprintln!($($arg)*);
        }
    };
}

#[macro_export]
macro_rules! log_warn {
    ($($arg:tt)*) => {
        #[cfg(feature = "tracing")]
        {
            ::tracing::warn!($($arg)*);
        }
        #[cfg(not(feature = "tracing"))]
        {
            eprintln!($($arg)*);
        }
    };
}

#[macro_export]
macro_rules! log_debug {
    ($($arg:tt)*) => {
        #[cfg(feature = "tracing")]
        {
            ::tracing::debug!($($arg)*);
        }
        #[cfg(not(feature = "tracing"))]
        {
            println!($($arg)*);
        }
    };
}
