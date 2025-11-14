package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	agent "github.com/malbeclabs/doublezero/controlplane/agent/internal/agent"
	arista "github.com/malbeclabs/doublezero/controlplane/agent/pkg/arista"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	localDevicePubkey          = flag.String("pubkey", "frtyt4WKYudUpqTsvJzwN6Bd4btYxrkaYNhBNAaUVGWn", "This device's public key on the doublezero network")
	controllerAddress          = flag.String("controller", "18.116.166.35:7000", "The DoubleZero controller IP address and port to connect to")
	device                     = flag.String("device", "127.0.0.1:9543", "IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543.")
	sleepIntervalInSeconds     = flag.Float64("sleep-interval-in-seconds", 5, "How long to sleep in between polls")
	controllerTimeoutInSeconds = flag.Float64("controller-timeout-in-seconds", 2, "How long to wait for a response from the controller before giving up")
	maxLockAge                 = flag.Int("max-lock-age-in-seconds", 3600, "If agent detects a config lock that older than the specified age, it will force unlock.")
	verbose                    = flag.Bool("verbose", false, "Enable verbose logging")
	showVersion                = flag.Bool("version", false, "Print the version of the doublezero-agent and exit")
	metricsEnable              = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr                = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics")

	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func pollControllerAndConfigureDevice(ctx context.Context, dzclient pb.ControllerClient, eapiClient *arista.EAPIClient, pubkey string, verbose *bool, maxLockAge int, agentVersion string, agentCommit string, agentDate string) error {
	var err error

	// The dz controller needs to know what BGP sessions we have configured locally
	var neighborIpMap map[string][]string
	neighborIpMap, err = eapiClient.GetBgpNeighbors(ctx)
	if err != nil {
		log.Println("pollControllerAndConfigureDevice: eapiClient.GetBgpNeighbors returned error:", err)
		agent.ErrorsBgpNeighbors.Inc()
	}

	var configText string
	configText, err = agent.GetConfigFromServer(ctx, dzclient, pubkey, neighborIpMap, controllerTimeoutInSeconds, agentVersion, agentCommit, agentDate)
	if err != nil {
		log.Printf("pollControllerAndConfigureDevice failed to call agent.GetConfigFromServer: %q", err)
		agent.ErrorsGetConfig.Inc()
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
		agent.ErrorsApplyConfig.Inc()
		return err
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile | log.Lmicroseconds)

	log.Printf("Starting doublezero-agent version %s starting\n", version)
	log.Printf("doublezero-agent pubkey: %s\n", *localDevicePubkey)
	log.Printf("doublezero-agent controller: %s\n", *controllerAddress)
	log.Printf("doublezero-agent device: %s\n", *device)
	log.Printf("doublezero-agent sleep-interval-in-seconds: %f\n", *sleepIntervalInSeconds)
	log.Printf("doublezero-agent controller-timeout-in-seconds: %f\n", *controllerTimeoutInSeconds)
	log.Printf("doublezero-agent max-lock-age-in-seconds: %d\n", *maxLockAge)

	if *metricsEnable {
		agent.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(*metricsAddr, nil); err != nil {
				log.Printf("Failed to start prometheus metrics server: %v", err)
			}
		}()
	}

	dzclient, err := agent.GetDzClient(*controllerAddress)
	if err != nil {
		log.Fatalf("Call to GetDzClient failed: %q\n", err)
	}

	ticker := time.NewTicker(time.Duration(*sleepIntervalInSeconds * float64(time.Second)))

	var eapiClient *arista.EAPIClient

	clientConn, err := arista.NewClientConn(*device)
	if err != nil {
		log.Fatalf("call to NewClientConn failed: %v\n", err)
	}

	client := aristapb.NewEapiMgrServiceClient(clientConn)
	eapiClient = arista.NewEAPIClient(slog.Default(), client)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := pollControllerAndConfigureDevice(ctx, dzclient, eapiClient, *localDevicePubkey, verbose, *maxLockAge, version, commit, date)
			if err != nil {
				log.Println("ERROR: pollAndConfigureDevice returned", err)
			}
		}
	}
}
