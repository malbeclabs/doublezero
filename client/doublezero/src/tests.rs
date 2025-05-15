#[cfg(test)]
mod tests {
    use assert_cmd::Command;

    #[test]
    fn test_cli_no_arguments() {
        let mut cmd = Command::cargo_bin("doublezero").unwrap();
        cmd.assert().failure().code(2);
    }
}
