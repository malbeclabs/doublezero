use crate::servicecontroller::{RouteRecord, ServiceController};

pub async fn retrieve_routes<T: ServiceController>(
    controller: &T,
    spinner: Option<&indicatif::ProgressBar>,
) -> eyre::Result<Vec<RouteRecord>> {
    if let Some(spinner) = spinner {
        spinner.set_message("Retrieving routes...");
    }

    let get_routes = || async {
        let routes = controller.routes().await.map_err(|e| eyre::eyre!(e))?;

        match routes.len() {
            0 => Err(eyre::eyre!("No routes found")),
            _ => Ok(routes),
        }
    };

    let routes = get_routes().await?;

    Ok(routes)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{MockServiceController, RouteRecord};

    fn make_route(local_ip: &str, peer_ip: &str) -> RouteRecord {
        RouteRecord {
            network: "test".to_string(),
            local_ip: local_ip.to_string(),
            kernel_state: "present".to_string(),
            liveness_last_updated: None,
            liveness_state: None,
            liveness_state_reason: None,
            peer_ip: peer_ip.to_string(),
            peer_client_version: None,
        }
    }

    #[tokio::test]
    async fn test_retrieve_routes() {
        let routes = vec![
            make_route("192.168.1.1", "192.168.1.2"),
            make_route("192.168.1.2", "192.168.1.3"),
            make_route("192.168.1.3", "192.168.1.4"),
        ];

        let mut controller = MockServiceController::new();
        let expected_routes = routes.clone();
        controller
            .expect_routes()
            .returning(move || Ok(expected_routes.clone()));

        let result = retrieve_routes(&controller, None).await.unwrap();
        assert_eq!(result.len(), 3);
        assert_eq!(result, routes);
    }
}
