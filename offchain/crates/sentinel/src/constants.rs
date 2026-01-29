pub const ENV_PREVIOUS_LEADER_EPOCHS: u8 = 1;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_leader_schedule_lookahead_is_one_epoch() {
        assert_eq!(ENV_PREVIOUS_LEADER_EPOCHS, 1);
    }
}
