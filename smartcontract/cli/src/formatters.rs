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
