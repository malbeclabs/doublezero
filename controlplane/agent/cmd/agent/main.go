package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	agent "github.com/malbeclabs/doublezero/controlplane/agent/internal/agent"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
)

var (
	localDevicePubkey          = flag.String("pubkey", "frtyt4WKYudUpqTsvJzwN6Bd4btYxrkaYNhBNAaUVGWn", "This device's public key on the doublezero network")
	controllerAddress          = flag.String("controller", "18.116.166.35:7000", "The DoubleZero controller IP address and port to connect to")
	device                     = flag.String("device", "127.0.0.1:9543", "IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543.")
	sleepIntervalInSeconds     = flag.Float64("sleep-interval-in-seconds", 5, "How long to sleep in between polls")
	controllerTimeoutInSeconds = flag.Float64("controller-timeout-in-seconds", 2, "How long to wait for a response from the controller before giving up")
	maxLockAge                 = flag.Int("max-lock-age-in-seconds", 3600, "If agent detects a config lock that older than the specified age, it will force unlock.")
	verbose                    = flag.Bool("verbose", false, "Enable verbose logging")

	version = flag.Bool("version", false, "version info")
	Build   string
)

func pollControllerAndConfigureDevice(ctx context.Context, dzclient pb.ControllerClient, device *string, pubkey string, verbose *bool, maxLockAge int) error {
	var eapiClient *agent.EapiClient
	var err error

	eapiClient, err = agent.NewEapiClient(*device, nil)
	if err != nil {
		return err
	}
	// The dz controller needs to know what BGP sessions we have configured locally
	var neighborIpMap map[string][]string
	neighborIpMap, err = eapiClient.GetBgpNeighbors(ctx)
	if err != nil {
		log.Println("pollControllerAndConfigureDevice: eapiClient.GetBgpNeighbors returned error:", err)
	}

	var configText string
	configText, err = agent.GetConfigFromServer(ctx, dzclient, pubkey, neighborIpMap, controllerTimeoutInSeconds)
	if err != nil {
		log.Printf("pollControllerAndConfigureDevice failed to call agent.GetConfigFromServer: %q", err)
		return err
	}

	if *verbose {
		log.Printf("controller returned the following config: '%s'", configText)
	}

	if configText == "" {
		// Controller returned empty config
		return nil
	}

	_, err = eapiClient.AddConfigToDevice(ctx, configText, nil, maxLockAge) // 3rd arg (diffCmd) is only used for testing
	if err != nil {
		return err
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	flag.Parse()

	if Build == "" {
		Build = "unknown"
	}

	if *version {
		fmt.Printf("build: %s\n", Build)
		os.Exit(0)
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile | log.Lmicroseconds)

	log.Printf("Starting doublezero-agent version %s starting\n", Build)
	log.Printf("doublezero-agent pubkey: %s\n", *localDevicePubkey)
	log.Printf("doublezero-agent controller: %s\n", *controllerAddress)
	log.Printf("doublezero-agent device: %s\n", *device)
	log.Printf("doublezero-agent sleep-interval-in-seconds: %f\n", *sleepIntervalInSeconds)
	log.Printf("doublezero-agent controller-timeout-in-seconds: %f\n", *controllerTimeoutInSeconds)
	log.Printf("doublezero-agent max-lock-age-in-seconds: %d\n", *maxLockAge)

	dzclient, err := agent.GetDzClient(*controllerAddress)
	if err != nil {
		log.Fatalf("Call to GetDzClient failed: %q\n", err)
	}

	ticker := time.NewTicker(time.Duration(*sleepIntervalInSeconds * float64(time.Second)))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := pollControllerAndConfigureDevice(ctx, dzclient, device, *localDevicePubkey, verbose, *maxLockAge)
			if err != nil {
				log.Println("ERROR: pollAndConfigureDevice returned", err)
			}
		}
	}
}
