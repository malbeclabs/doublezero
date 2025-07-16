use rand::Rng;

pub fn nextindex() -> u128 {
    let mut rng = rand::thread_rng();
    rng.gen::<u128>()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generates_different_values() {
        let a = nextindex();
        let b = nextindex();

        assert_ne!(a, b);
    }
}
