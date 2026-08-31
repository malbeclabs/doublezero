package qa

import (
	"github.com/gagliardetto/solana-go"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

func (c *Client) currentSolanaRPCURL() string {
	if c.solanaRPC != nil {
		return c.solanaRPC.CurrentURL()
	}
	return c.SolanaRPCURL
}

func (c *Client) scrubRPCErr(err error) string {
	if err == nil {
		return ""
	}
	if c.solanaRPC != nil {
		return c.solanaRPC.scrubErr(err)
	}
	return err.Error()
}

func (c *Client) shredsClient(programID solana.PublicKey) *shreds.Client {
	if c.solanaRPC != nil {
		return shreds.New(c.solanaRPC.RPC(), programID)
	}
	return shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID)
}

func (c *Client) withReadFailover(fn func(rpcURL string) error) error {
	attempts := 1
	if c.solanaRPC != nil {
		attempts = c.solanaRPC.EndpointCount()
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		lastErr = fn(c.currentSolanaRPCURL())
		if lastErr == nil {
			return nil
		}
		if c.solanaRPC != nil && isRetryableRPCErr(lastErr) {
			c.log.Warn("Settlement query failed, failing over to next endpoint",
				"host", c.Host, "endpoint", redactURL(c.currentSolanaRPCURL()), "error", c.solanaRPC.scrubErr(lastErr))
			c.solanaRPC.Failover()
		} else {
			return lastErr
		}
	}
	return lastErr
}
