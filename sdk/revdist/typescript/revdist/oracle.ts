/** SOL/2Z oracle client. */

export interface SwapRate {
  rate: number;
  timestamp: number;
  signature: string;
  solPriceUsd: string;
  twozPriceUsd: string;
  cacheHit: boolean;
}

export class OracleClient {
  private readonly baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl;
  }

  async fetchSwapRate(): Promise<SwapRate> {
    const resp = await fetch(`${this.baseUrl}/swap-rate`);
    if (!resp.ok) {
      throw new Error(`oracle returned status ${resp.status}`);
    }
    const data = await resp.json();
    return {
      rate: data.swapRate,
      timestamp: data.timestamp,
      signature: data.signature,
      solPriceUsd: data.solPriceUsd,
      twozPriceUsd: data.twozPriceUsd,
      cacheHit: data.cacheHit,
    };
  }
}
