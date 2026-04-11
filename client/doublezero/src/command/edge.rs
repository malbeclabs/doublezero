use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct EdgeEnableCliCommand {
    /// Multicast group code
    #[arg(long)]
    pub code: String,

    /// Parser name (e.g. "topofbook")
    #[arg(long)]
    pub parser: String,

    /// Output format: "json" or "csv"
    #[arg(long, default_value = "json")]
    pub format: String,

    /// Output path (file path or "unix:///path/to/sock")
    #[arg(long)]
    pub output: String,

    /// UDP port for marketdata messages (quotes, trades)
    #[arg(long)]
    pub marketdata_port: u16,

    /// UDP port for refdata messages (instrument definitions)
    #[arg(long)]
    pub refdata_port: u16,
}

impl EdgeEnableCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        self.execute_with_service_controller(client, &controller)
            .await
    }

    pub async fn execute_with_service_controller<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<()> {
        check_doublezero(controller, client, None).await?;
        controller
            .edge_enable(
                &self.code,
                &self.parser,
                &self.format,
                &self.output,
                self.marketdata_port,
                self.refdata_port,
            )
            .await?;
        println!(
            "Edge feed enabled: code={}, parser={}, format={}, output={}",
            self.code, self.parser, self.format, self.output
        );
        Ok(())
    }
}

#[derive(Args, Debug)]
pub struct EdgeDisableCliCommand {
    /// Multicast group code
    #[arg(long)]
    pub code: String,
}

impl EdgeDisableCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        self.execute_with_service_controller(client, &controller)
            .await
    }

    pub async fn execute_with_service_controller<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<()> {
        check_doublezero(controller, client, None).await?;
        controller.edge_disable(&self.code).await?;
        println!("Edge feed disabled: code={}", self.code);
        Ok(())
    }
}

#[derive(Args, Debug)]
pub struct EdgeStatusCliCommand {
    /// Output as JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
}

impl EdgeStatusCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        self.execute_with_service_controller(client, &controller)
            .await
    }

    pub async fn execute_with_service_controller<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<()> {
        check_doublezero(controller, client, None).await?;
        let statuses = controller.edge_status().await?;

        if self.json {
            println!("{}", serde_json::to_string_pretty(&statuses)?);
            return Ok(());
        }

        if statuses.is_empty() {
            println!("No active edge feeds");
            return Ok(());
        }

        for s in &statuses {
            println!(
                "  {} parser={} format={} output={} records={} buffered={} running={}",
                s.code, s.parser, s.format, s.output, s.records_written, s.buffered, s.running,
            );
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{EdgeFeedStatus, MockServiceController};
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_config::Environment;

    fn setup_mock() -> MockServiceController {
        let mut mock = MockServiceController::new();
        mock.expect_service_controller_check().return_const(true);
        mock.expect_service_controller_can_open().return_const(true);
        mock.expect_get_env()
            .returning_st(|| Ok(Environment::default()));
        mock
    }

    fn setup_client() -> doublezero_cli::doublezerocommand::MockCliCommand {
        let mut client = create_test_client();
        client
            .expect_get_environment()
            .returning_st(Environment::default);
        client
    }

    #[tokio::test]
    async fn test_edge_enable_success() {
        let mut mock = setup_mock();
        mock.expect_edge_enable()
            .returning(|_, _, _, _, _, _| Ok(()));

        let client = setup_client();
        let cmd = EdgeEnableCliCommand {
            code: "mg01".to_string(),
            parser: "topofbook".to_string(),
            format: "json".to_string(),
            output: "/tmp/test.jsonl".to_string(),
            marketdata_port: 7000,
            refdata_port: 7001,
        };
        let result = cmd.execute_with_service_controller(&client, &mock).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_edge_disable_success() {
        let mut mock = setup_mock();
        mock.expect_edge_disable().returning(|_| Ok(()));

        let client = setup_client();
        let cmd = EdgeDisableCliCommand {
            code: "mg01".to_string(),
        };
        let result = cmd.execute_with_service_controller(&client, &mock).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_edge_status_empty() {
        let mut mock = setup_mock();
        mock.expect_edge_status().returning(|| Ok(vec![]));

        let client = setup_client();
        let cmd = EdgeStatusCliCommand { json: false };
        let result = cmd.execute_with_service_controller(&client, &mock).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_edge_status_with_feeds() {
        let mut mock = setup_mock();
        mock.expect_edge_status().returning(|| {
            Ok(vec![EdgeFeedStatus {
                code: "mg01".to_string(),
                parser: "topofbook".to_string(),
                format: "json".to_string(),
                output: "/tmp/out.jsonl".to_string(),
                records_written: 42,
                buffered: 0,
                running: true,
            }])
        });

        let client = setup_client();
        let cmd = EdgeStatusCliCommand { json: true };
        let result = cmd.execute_with_service_controller(&client, &mock).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_edge_enable_daemon_not_running() {
        let mut mock = MockServiceController::new();
        mock.expect_service_controller_check().return_const(false);

        let client = setup_client();
        let cmd = EdgeEnableCliCommand {
            code: "mg01".to_string(),
            parser: "topofbook".to_string(),
            format: "json".to_string(),
            output: "/tmp/test.jsonl".to_string(),
            marketdata_port: 7000,
            refdata_port: 7001,
        };
        let result = cmd.execute_with_service_controller(&client, &mock).await;
        assert!(result.is_err());
    }
}
