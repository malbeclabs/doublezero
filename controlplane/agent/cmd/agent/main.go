package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	localDevicePubkey           = flag.String("pubkey", "frtyt4WKYudUpqTsvJzwN6Bd4btYxrkaYNhBNAaUVGWn", "This device's public key on the doublezero network")
	controllerAddress           = flag.String("controller", "18.116.166.35:7000", "The DoubleZero controller IP address and port to connect to")
	device                      = flag.String("device", "127.0.0.1:9543", "IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543.")
	sleepIntervalInSeconds      = flag.Float64("sleep-interval-in-seconds", 5, "How long to sleep in between polls")
	controllerTimeoutInSeconds  = flag.Float64("controller-timeout-in-seconds", 30, "How long to wait for a response from the controller before giving up")
	configCacheTimeoutInSeconds = flag.Int("config-cache-timeout-in-seconds", 60, "Force full config fetch after this many seconds, even if hash unchanged")
	maxLockAge                  = flag.Int("max-lock-age-in-seconds", 3600, "If agent detects a config lock that older than the specified age, it will force unlock.")
	verbose                     = flag.Bool("verbose", false, "Enable verbose logging")
	showVersion                 = flag.Bool("version", false, "Print the version of the doublezero-agent and exit")
	metricsEnable               = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr                 = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics")

	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func computeChecksum(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func fetchConfigFromController(ctx context.Context, dzclient pb.ControllerClient, pubkey string, neighborIpMap map[string][]string, verbose *bool, agentVersion string, agentCommit string, agentDate string) (configText string, configHash string, err error) {
	configText, err = agent.GetConfigFromServer(ctx, dzclient, pubkey, neighborIpMap, controllerTimeoutInSeconds, agentVersion, agentCommit, agentDate)
	if err != nil {
		log.Printf("fetchConfigFromController failed to call agent.GetConfigFromServer: %q", err)
		agent.ErrorsGetConfig.Inc()
		return "", "", err
	}

	if *verbose {
		log.Printf("controller returned the following config: '%s'", configText)
	}

	configHash = computeChecksum(configText)
	return configText, configHash, nil
}

func applyConfig(ctx context.Context, eapiClient *arista.EAPIClient, configText string, maxLockAge int) error {
	if configText == "" {
		return nil
	}

	_, err := eapiClient.AddConfigToDevice(ctx, configText, nil, maxLockAge)
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

	var cachedConfigHash string
	var configCacheTime time.Time
	configCacheTimeout := time.Duration(*configCacheTimeoutInSeconds) * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			neighborIpMap, err := eapiClient.GetBgpNeighbors(ctx)
			if err != nil {
				log.Println("ERROR: eapiClient.GetBgpNeighbors returned", err)
				agent.ErrorsBgpNeighbors.Inc()
			}

			shouldFetchAndApply := false

			if cachedConfigHash == "" {
				shouldFetchAndApply = true
			} else if time.Since(configCacheTime) >= configCacheTimeout {
				shouldFetchAndApply = true
			} else {
				hash, err := agent.GetConfigHashFromServer(ctx, dzclient, *localDevicePubkey, neighborIpMap, controllerTimeoutInSeconds, version, commit, date)
				if err != nil {
					log.Println("ERROR: GetConfigHashFromServer returned", err)
					continue
				}
				if hash != cachedConfigHash {
					shouldFetchAndApply = true
				}
			}

			if !shouldFetchAndApply {
				continue
			}

			configText, configHash, err := fetchConfigFromController(ctx, dzclient, *localDevicePubkey, neighborIpMap, verbose, version, commit, date)
			if err != nil {
				log.Println("ERROR: fetchConfigFromController returned", err)
				continue
			}

			err = applyConfig(ctx, eapiClient, configText, *maxLockAge)
			if err != nil {
				log.Println("ERROR: applyConfig returned", err)
				continue
			}
			cachedConfigHash = configHash
			configCacheTime = time.Now()
		}
	}
}
