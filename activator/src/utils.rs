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
        .map(|(k, v)| format!("{}={}", k, escape_characters(v)))
        .collect::<Vec<String>>()
        .join(",")
}

fn escape_characters<T: std::fmt::Display>(value: &T) -> String {
    let value_str = value.to_string();
    value_str
        .replace(' ', "\\ ")
        .replace(',', "\\,")
        .replace('=', "\\=")
        .replace('"', "\\\"")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_kvpair_string() {
        let map = std::collections::BTreeMap::from([("a", 1), ("b", 2), ("c", 3)]);

        let result = kvpair_string(&map);
        assert_eq!(result, "a=1,b=2,c=3");
    }

    #[test]
    fn test_kvpair_string_with_special_characters() {
        let map = std::collections::BTreeMap::from([
            ("key1", "value with space"),
            ("key2", "value\"with\"quotes"),
            ("key3", "value,with,comma"),
            ("key4", "value=with=equals"),
        ]);

        let result = kvpair_string(&map);
        assert_eq!(result, "key1=value\\ with\\ space,key2=value\\\"with\\\"quotes,key3=value\\,with\\,comma,key4=value\\=with\\=equals");
    }

    #[test]
    fn test_escape_characters() {
        let input = "Hello, \"World!\"=";
        let expected = "Hello\\,\\ \\\"World!\\\"\\=";
        assert_eq!(escape_characters(&input), expected);
    }
}
