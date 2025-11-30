package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	flag "github.com/spf13/pflag"
)

func main() {
	timeout := flag.Duration("timeout", 10*time.Second, "overall timeout for probe (e.g. 1s, 10s)")
	rpcURL := flag.String("rpc", "https://api.mainnet-beta.solana.com", "Solana JSON-RPC endpoint to poll for the test transaction")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Printf("Usage: %s [flags] <tpu-ip>:<tpu-port>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}
	dstAddr := flag.Arg(0)

	if err := probeTPU(dstAddr, *rpcURL, *timeout); err != nil {
		fmt.Printf("TPU probe failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("TPU probe succeeded: test transaction was observed by RPC (likely failed, as intended)")
}

func probeTPU(dstAddr, rpcURL string, timeout time.Duration) error {
	udpAddr, err := net.ResolveUDPAddr("udp", dstAddr)
	if err != nil {
		return fmt.Errorf("resolve TPU addr: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("dial TPU UDP: %w", err)
	}
	defer conn.Close()
	log.Info("dialed TPU UDP", "local_addr", conn.LocalAddr(), "remote_addr", conn.RemoteAddr())

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rpcClient := rpc.New(rpcURL)

	// Get a recent blockhash so the tx is valid enough to be processed.
	latest, err := rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get latest blockhash: %w", err)
	}
	log.Info("got latest blockhash", "blockhash", latest.Value.Blockhash)

	// Ephemeral payer with zero lamports; tx will fail but still be recorded if it reaches the validator.
	payer, err := solana.NewRandomPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to create random private key: %w", err)
	}
	log.Info("created ephemeral payer", "pubkey", payer.PublicKey())

	// Use a random fake program ID so the tx is guaranteed to fail.
	fakeProg, err := solana.NewRandomPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to create random private key (program): %w", err)
	}
	instruction := solana.NewInstruction(
		fakeProg.PublicKey(),
		nil,
		[]byte{0x01}, // arbitrary data
	)
	log.Info("created instruction", "program", fakeProg.PublicKey())

	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		latest.Value.Blockhash,
		solana.TransactionPayer(payer.PublicKey()),
	)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	log.Info("created transaction", "tx", tx)

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(payer.PublicKey()) {
			return &payer
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}
	log.Info("signed transaction", "tx", tx)

	wire, err := tx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshall transaction: %w", err)
	}
	log.Info("marshalled transaction", "tx", tx)

	if _, err := conn.Write(wire); err != nil {
		return fmt.Errorf("failed to write to TPU: %w", err)
	}
	log.Info("wrote to TPU", "tx", tx)

	sig := tx.Signatures[0]

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for signature %s to appear in RPC", sig.String())
		}

		log.Info("getting signature statuses", "sig", sig.String())
		out, err := rpcClient.GetSignatureStatuses(
			context.Background(),
			false, // searchTransactionHistory
			sig,
		)
		if err != nil {
			return fmt.Errorf("failed to get signature statuses: %w", err)
		}

		log.Info("got signature statuses", "sig", sig.String(), "status", out.Value[0])
		if len(out.Value) > 0 && out.Value[0] != nil {
			// Status found — tx made it from you -> TPU -> validator -> RPC.
			// It will almost certainly be Err != nil (insufficient funds / bad program),
			// but that’s exactly what we want.
			log.Info("signature status found", "sig", sig.String())
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}
