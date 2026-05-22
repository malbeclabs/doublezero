//! Shared display formatters used by CLI verbs to render table output.
//!
//! Per RFC-20 (§Output conventions): "Modules SHOULD use shared display
//! helpers from the CLI core crate for common types (pubkey, bandwidth,
//! latency, IPv4)." This module is the home for those helpers; today it
//! exposes the `DisplayVec` helper and `stringify_vec` shared by several
//! verbs. Type-specific formatters (bandwidth, latency, IPv4, pubkey) move
//! here as they are touched.

use std::fmt::{self, Display};

pub struct DisplayVec<'a, T: Display>(pub &'a Vec<T>);

impl<'a, T: Display> Display for DisplayVec<'a, T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut iter = self.0.iter();
        if let Some(first) = iter.next() {
            write!(f, "{first}")?;
            for item in iter {
                write!(f, ",{item}")?;
            }
        }
        Ok(())
    }
}

impl<'a, T: Display> From<&'a Vec<T>> for DisplayVec<'a, T> {
    fn from(vec: &'a Vec<T>) -> Self {
        DisplayVec(vec)
    }
}

pub fn stringify_vec<T: Display>(v: &Vec<T>) -> String {
    format!("{}", DisplayVec(v))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn display_vec_joins_with_comma() {
        let v = vec![1, 2, 3];
        assert_eq!(stringify_vec(&v), "1,2,3");
    }

    #[test]
    fn display_vec_handles_empty() {
        let v: Vec<i32> = vec![];
        assert_eq!(stringify_vec(&v), "");
    }

    #[test]
    fn display_vec_handles_singleton() {
        let v = vec!["only"];
        assert_eq!(stringify_vec(&v), "only");
    }
}
