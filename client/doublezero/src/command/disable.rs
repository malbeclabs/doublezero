use crate::{
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct DisableCliCommand {}

impl DisableCliCommand {
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

        if let Ok(v2) = controller.v2_status().await {
            if !v2.reconciler_enabled {
                println!("Reconciler already disabled");
                return Ok(());
            }
            // Check if any services are active and warn the user.
            let has_active = v2.services.iter().any(|s| s.user_type.is_some());
            if has_active {
                println!("Active tunnel(s) will be torn down");
            }
        }

        controller.disable().await?;
        println!("Reconciler disabled");
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{MockServiceController, V2StatusResponse};
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_config::Environment;

    fn setup_mock() -> MockServiceController {
        let mut mock = MockServiceController::new();
        mock.expect_service_controller_check().return_const(true);
        mock.expect_service_controller_can_open().return_const(true);
        mock.expect_get_env()
            .returning_st(|| Ok(Environment::default()));
        mock.expect_v2_status().returning(|| {
            Ok(V2StatusResponse {
                reconciler_enabled: true,
                client_ip: String::new(),
                services: vec![],
            })
        });
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
    async fn test_disable_command_success() {
        let mut mock = setup_mock();
        mock.expect_disable().returning(|| Ok(()));

        let client = setup_client();
        let command = DisableCliCommand {};
        let result = command
            .execute_with_service_controller(&client, &mock)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_disable_command_daemon_error() {
        let mut mock = setup_mock();
        mock.expect_disable()
            .returning(|| Err(eyre::eyre!("connection refused")));

        let client = setup_client();
        let command = DisableCliCommand {};
        let result = command
            .execute_with_service_controller(&client, &mock)
            .await;
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("connection refused"));
    }

    #[tokio::test]
    async fn test_disable_command_daemon_not_running() {
        let mut mock = MockServiceController::new();
        mock.expect_service_controller_check().return_const(false);

        let client = setup_client();
        let command = DisableCliCommand {};
        let result = command
            .execute_with_service_controller(&client, &mock)
            .await;
        assert!(result.is_err());
    }
}
