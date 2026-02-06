import { Connection, type ConnectionConfig } from "@solana/web3.js";

const DEFAULT_MAX_RETRIES = 5;

/**
 * Creates a Solana RPC Connection with retry on 429 Too Many Requests.
 *
 * The built-in @solana/web3.js retry uses short backoffs (500ms-4s) that
 * may not be sufficient for rate-limited public RPC endpoints. This wrapper
 * provides longer backoff intervals (2s, 4s, 6s, 8s, 10s).
 */
export function newConnection(
  url: string,
  config?: ConnectionConfig & { maxRetries?: number },
): Connection {
  const maxRetries = config?.maxRetries ?? DEFAULT_MAX_RETRIES;
  const retryFetch = async (
    input: Parameters<typeof fetch>[0],
    init?: Parameters<typeof fetch>[1],
  ): Promise<Response> => {
    for (let attempt = 0; ; attempt++) {
      const response = await fetch(input, init);
      if (response.status !== 429 || attempt >= maxRetries) {
        return response;
      }
      await new Promise((resolve) =>
        setTimeout(resolve, (attempt + 1) * 2000),
      );
    }
  };
  return new Connection(url, {
    ...config,
    disableRetryOnRateLimit: true,
    fetch: retryFetch as typeof fetch,
  });
}
