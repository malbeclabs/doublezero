use std::time::{SystemTime, UNIX_EPOCH};

pub fn get_utc_nanoseconds_since_epoch() -> u128 {
    match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(duration) => duration.as_nanos(),
        Err(_) => {
            panic!("Can't get ns since epoch");
        }
    }
}

/// Formats a container of key-value pairs into a comma-separated string.
///
/// This function takes a container that can be converted into an iterator of borrowed
/// key-value pairs and formats each pair as "key=value". The resulting strings
/// are then joined together with commas.
///
/// # Arguments
///
/// * `container`: A container that can be converted into an iterator of borrowed
///     key-value pairs. Examples include [`HashMap`], [`BTreeMap`], and slices of tuples.
///
/// # Returns
///
/// A [`String`] where each key-value pair from the container is formatted as
/// "key=value" and the pairs are separated by commas. If the container is empty,
/// an empty string is returned.
///
/// # Examples
///
/// ```
/// use std::collections::HashMap;
///
/// let map = HashMap::from([
///     ("one", 1),
///     ("two", 2),
///     ("three", 3),
/// ]);
///
/// let result = kvpair_string(&map);
/// assert_eq!(result, "one=1,two=2,three=3"); // Order might vary for HashMap
/// ```
pub fn kvpair_string<'a, K, V, I>(container: I) -> String
where
    K: std::fmt::Display + 'a,
    V: std::fmt::Display + 'a,
    I: IntoIterator<Item = (&'a K, &'a V)>,
{
    container
        .into_iter()
        .map(|(k, v)| format!("{}={}", k, v))
        .collect::<Vec<String>>()
        .join(",")
}
