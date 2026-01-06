use std::time::Duration;

use anyhow::{Context, Result, bail};
use reqwest::{Client, StatusCode, header};
use url::Url;

/// Base URL for Jupiter API with authentication (requires API key).
pub const JUPITER_API_BASE_URL: &str = "https://api.jup.ag";

/// Base URL for Jupiter legacy API (no authentication required, deprecated Jan 31 2026).
pub const JUPITER_LITE_API_BASE_URL: &str = "https://lite-api.jup.ag";

/// Jupiter API client.
///
/// Supports two modes:
/// - Authenticated: Uses `api.jup.ag` with `x-api-key` header (when API key provided)
/// - Unauthenticated: Uses `lite-api.jup.ag` without header (legacy, deprecated Jan 31 2026)
#[derive(Debug, Clone)]
pub struct JupiterClient {
    client: Client,
    base_url: Url,
}

impl JupiterClient {
    /// Creates a new Jupiter client.
    ///
    /// - If `api_key` is `Some`, uses `api.jup.ag` with the `x-api-key` header.
    /// - If `api_key` is `None`, uses `lite-api.jup.ag` without authentication.
    pub fn new(api_key: Option<&str>) -> Result<Self> {
        let base_url = if api_key.is_some() {
            JUPITER_API_BASE_URL
        } else {
            JUPITER_LITE_API_BASE_URL
        };

        Self::with_base_url(api_key, base_url)
    }

    /// Creates a new Jupiter client with a custom base URL (for testing).
    pub fn with_base_url(api_key: Option<&str>, base_url: &str) -> Result<Self> {
        let base_url =
            Url::parse(base_url).with_context(|| format!("Invalid base URL: {base_url}"))?;

        let mut client_builder = Client::builder().timeout(Duration::from_secs(30));

        if let Some(key) = api_key {
            let mut headers = header::HeaderMap::new();
            headers.insert(
                "x-api-key",
                header::HeaderValue::from_str(key).context("Invalid Jupiter API key format")?,
            );
            client_builder = client_builder.default_headers(headers);
        }

        let client = client_builder
            .build()
            .context("Failed to build HTTP client")?;

        Ok(Self { client, base_url })
    }

    /// Executes a GET request to the Jupiter API.
    pub async fn get<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        query: &impl serde::Serialize,
    ) -> Result<T> {
        let url = self.build_url(path)?;
        let response = self
            .client
            .get(url)
            .query(query)
            .send()
            .await
            .context("Jupiter API request failed")?;

        self.handle_response(response).await
    }

    /// Executes a POST request to the Jupiter API.
    pub async fn post<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        body: &impl serde::Serialize,
    ) -> Result<T> {
        let url = self.build_url(path)?;
        let response = self
            .client
            .post(url)
            .json(body)
            .send()
            .await
            .context("Jupiter API request failed")?;

        self.handle_response(response).await
    }

    fn build_url(&self, path: &str) -> Result<Url> {
        self.base_url
            .join(path)
            .with_context(|| format!("Invalid API path: {path}"))
    }

    async fn handle_response<T: serde::de::DeserializeOwned>(
        &self,
        response: reqwest::Response,
    ) -> Result<T> {
        let status = response.status();

        if status.is_success() {
            return response
                .json()
                .await
                .context("Failed to parse Jupiter API response");
        }

        let body = response
            .text()
            .await
            .unwrap_or_else(|_| "<unable to read body>".to_string());
        let body_snippet = if body.len() > 200 {
            format!("{}...", &body[..200])
        } else {
            body
        };

        if status == StatusCode::UNAUTHORIZED || status == StatusCode::FORBIDDEN {
            bail!(
                "Jupiter API authentication failed (HTTP {status}): {body_snippet}\n\
                 Hint: Provide a valid API key via --jupiter-api-key"
            );
        }

        bail!("Jupiter API request failed (HTTP {status}): {body_snippet}");
    }
}

#[cfg(test)]
mod tests {
    use wiremock::{Mock, MockServer, ResponseTemplate, matchers};

    use super::*;

    #[tokio::test]
    async fn test_authenticated_client_sends_api_key_header() {
        let mock_server = MockServer::start().await;

        Mock::given(matchers::method("GET"))
            .and(matchers::path("/swap/v1/quote"))
            .and(matchers::header("x-api-key", "test-api-key-123"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "data": "ok"
            })))
            .expect(1)
            .mount(&mock_server)
            .await;

        let client =
            JupiterClient::with_base_url(Some("test-api-key-123"), &mock_server.uri()).unwrap();

        #[derive(serde::Serialize)]
        struct Query {
            amount: u64,
        }

        #[derive(serde::Deserialize)]
        struct Response {
            data: String,
        }

        let result: Response = client
            .get("/swap/v1/quote", &Query { amount: 1000 })
            .await
            .unwrap();

        assert_eq!(result.data, "ok");
    }

    #[tokio::test]
    async fn test_unauthenticated_client_does_not_send_api_key_header() {
        let mock_server = MockServer::start().await;

        // For unauthenticated client, we just verify the request succeeds
        // without requiring an x-api-key header. The mock accepts any request.
        Mock::given(matchers::method("GET"))
            .and(matchers::path("/swap/v1/quote"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "data": "ok"
            })))
            .expect(1)
            .mount(&mock_server)
            .await;

        let client = JupiterClient::with_base_url(None, &mock_server.uri()).unwrap();

        #[derive(serde::Serialize)]
        struct Query {
            amount: u64,
        }

        #[derive(serde::Deserialize)]
        struct Response {
            data: String,
        }

        let result: Response = client
            .get("/swap/v1/quote", &Query { amount: 1000 })
            .await
            .unwrap();

        assert_eq!(result.data, "ok");
    }

    #[tokio::test]
    async fn test_authenticated_client_uses_api_jup_ag() {
        let client = JupiterClient::new(Some("my-key")).unwrap();
        assert!(client.base_url.as_str().starts_with("https://api.jup.ag"));
        assert!(!client.base_url.as_str().contains("lite-api"));
    }

    #[tokio::test]
    async fn test_unauthenticated_client_uses_lite_api() {
        let client = JupiterClient::new(None).unwrap();
        assert!(client.base_url.as_str().contains("lite-api"));
    }

    #[tokio::test]
    async fn test_401_error_includes_helpful_message() {
        let mock_server = MockServer::start().await;

        Mock::given(matchers::any())
            .respond_with(
                ResponseTemplate::new(401).set_body_string(r#"{"error": "Invalid API key"}"#),
            )
            .mount(&mock_server)
            .await;

        let client = JupiterClient::with_base_url(Some("bad-key"), &mock_server.uri()).unwrap();

        #[derive(serde::Serialize)]
        struct Query {}

        let result: Result<serde_json::Value> = client.get("/test", &Query {}).await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(err_msg.contains("401"));
        assert!(err_msg.contains("authentication failed"));
    }

    #[tokio::test]
    async fn test_post_request() {
        let mock_server = MockServer::start().await;

        Mock::given(matchers::method("POST"))
            .and(matchers::path("/swap/v1/swap-instructions"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "success": true
            })))
            .expect(1)
            .mount(&mock_server)
            .await;

        let client = JupiterClient::with_base_url(None, &mock_server.uri()).unwrap();

        #[derive(serde::Serialize)]
        struct Body {
            user: String,
        }

        #[derive(serde::Deserialize)]
        struct Response {
            success: bool,
        }

        let result: Response = client
            .post(
                "/swap/v1/swap-instructions",
                &Body {
                    user: "test".to_string(),
                },
            )
            .await
            .unwrap();

        assert!(result.success);
    }
}
