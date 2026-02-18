package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"

	controllerpb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	outDir := flag.String("out", "devices_out", "output directory for device configs")
	controllerAddr := flag.String("controller", "localhost:50051", "controller gRPC address")
	env := flag.String("env", "devnet", "environment: devnet, testnet, or mainnet")
	flag.Parse()

	ctx := context.Background()

	networkConfig, err := config.NetworkConfigForEnv(*env)
	if err != nil {
		log.Fatalf("failed to get network config for env %s: %v", *env, err)
	}

	rpcClient := rpc.New(networkConfig.LedgerPublicRPCURL)
	svcClient := serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID)

	data, err := svcClient.GetProgramData(ctx)
	if err != nil {
		log.Fatalf("failed to fetch program data: %v", err)
	}

	conn, err := grpc.NewClient(*controllerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to controller gRPC: %v", err)
	}
	defer conn.Close()
	controllerClient := controllerpb.NewControllerClient(conn)

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	for _, device := range data.Devices {
		pubkeyStr := base58.Encode(device.PubKey[:])
		req := &controllerpb.ConfigRequest{Pubkey: pubkeyStr}
		resp, err := controllerClient.GetConfig(ctx, req)
		if err != nil {
			log.Printf("failed to fetch config for device %s: %v", device.Code, err)
			continue
		}
		outPath := filepath.Join(*outDir, fmt.Sprintf("%s.json", device.Code))
		if err := os.WriteFile(outPath, []byte(resp.GetConfig()), 0644); err != nil {
			log.Printf("failed to write device file %s: %v", outPath, err)
			continue
		}
		log.Printf("Wrote config for device %s to %s", device.Code, outPath)
		time.Sleep(250 * time.Millisecond)
	}
	log.Printf("Done. Device configs written to %s", *outDir)
}
