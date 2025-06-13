package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	agent "github.com/malbeclabs/doublezero/controlplane/agent/internal/agent"
	"github.com/malbeclabs/doublezero/controlplane/agent/internal/telemetry"
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

	// Telemetry flags
	telemetryEnabled            = flag.Bool("telemetry-enabled", true, "Enable telemetry collection")
	telemetryPort               = flag.Int("telemetry-port", telemetry.DefaultListenPort, "UDP port for telemetry service")
	telemetryInterval           = flag.Int("telemetry-interval", int(telemetry.DefaultSamplingInterval.Seconds()), "Sampling interval in seconds")
	telemetrySubmissionInterval = flag.Int("telemetry-submission-interval", int(telemetry.DefaultSubmissionInterval.Seconds()), "Submission interval in seconds")
	telemetryStoragePath        = flag.String("telemetry-storage-path", telemetry.DefaultStoragePath, "Storage path for telemetry data")
	localLocationPubkey         = flag.String("location-pubkey", "", "This device's location public key")
	telemetryPeersFile          = flag.String("telemetry-peers-file", "", "Path to JSON file containing telemetry peer configuration")

	version = flag.Bool("version", false, "version info")
	Build   string
)

func pollControllerAndConfigureDevice(ctx context.Context, dzclient pb.ControllerClient, eapiClient *agent.EapiClient, pubkey string, verbose *bool, maxLockAge int, telemetryCollector *telemetry.Collector) error {
	var err error

	// The dz controller needs to know what BGP sessions we have configured locally
	var neighborIpMap map[string][]string
	neighborIpMap, err = eapiClient.GetBgpNeighbors(ctx)
	if err != nil {
		log.Println("pollControllerAndConfigureDevice: eapiClient.GetBgpNeighbors returned error:", err)
	}

	var configText string
	var deviceData *agent.DeviceData
	configText, deviceData, err = agent.GetConfigFromServerWithMetadata(ctx, dzclient, pubkey, neighborIpMap, controllerTimeoutInSeconds)
	if err != nil {
		log.Printf("pollControllerAndConfigureDevice failed to call agent.GetConfigFromServerWithMetadata: %q", err)
		return err
	}

	if *verbose {
		log.Printf("controller returned the following config: '%s'", configText)
	}

	// Update telemetry peers if enabled and we have device data
	if telemetryCollector != nil && deviceData != nil {
		peers := extractPeersFromDeviceData(deviceData, neighborIpMap)
		// Only update peers if we actually got some from the controller
		// This prevents overwriting peers loaded from file with empty list
		if len(peers) > 0 {
			telemetryCollector.UpdatePeers(peers)
			if *verbose {
				log.Printf("Updated telemetry collector with %d peers from controller", len(peers))
			}
		}
	}

	// Also check if peers file has been updated (for testing and manual configuration)
	if telemetryCollector != nil && *telemetryPeersFile != "" {
		if _, err := os.Stat(*telemetryPeersFile); err == nil {
			peers, err := loadTelemetryPeers(*telemetryPeersFile)
			if err != nil {
				log.Printf("Warning: failed to reload telemetry peers from %s: %v", *telemetryPeersFile, err)
			} else if len(peers) > 0 {
				telemetryCollector.UpdatePeers(peers)
				if *verbose {
					log.Printf("Reloaded %d telemetry peers from %s", len(peers), *telemetryPeersFile)
				}
			}
		}
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

// extractPeersFromDeviceData extracts telemetry peers from device metadata
// Currently returns empty - peers are configured via the telemetry-peers-file flag
// In the future, this will fetch peers from the on-chain inventory via the controller
func extractPeersFromDeviceData(deviceData *agent.DeviceData, neighborIpMap map[string][]string) []*telemetry.PeerDevice {
	// This function is a placeholder for future integration with the serviceability smart contract
	// Per telemetry.md design:
	// - Peers are determined from on-chain inventory (devices and links)
	// - BGP state is irrelevant - we measure ALL links regardless of BGP status
	// - The controller will eventually provide this data

	// For now, return empty and rely on the JSON file configuration
	return []*telemetry.PeerDevice{}
}

// loadTelemetryPeers loads peer configuration from a JSON file
func loadTelemetryPeers(filename string) ([]*telemetry.PeerDevice, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read peers file: %w", err)
	}

	var config struct {
		Peers []struct {
			DevicePubkey   string `json:"device_pubkey"`
			LocationPubkey string `json:"location_pubkey"`
			IP             string `json:"ip"`
			LinkPubkey     string `json:"link_pubkey"`
			IsInternetPeer bool   `json:"is_internet_peer,omitempty"`
		} `json:"peers"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse peers file: %w", err)
	}

	peers := make([]*telemetry.PeerDevice, len(config.Peers))
	for i, p := range config.Peers {
		peers[i] = &telemetry.PeerDevice{
			DevicePubkey:   p.DevicePubkey,
			LocationPubkey: p.LocationPubkey,
			IP:             p.IP,
			LinkPubkey:     p.LinkPubkey,
			IsInternetPeer: p.IsInternetPeer,
		}
	}

	return peers, nil
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
	log.Printf("doublezero-agent telemetry-enabled: %v\n", *telemetryEnabled)
	if *telemetryEnabled {
		log.Printf("doublezero-agent telemetry-port: %d\n", *telemetryPort)
		log.Printf("doublezero-agent telemetry-interval: %d seconds\n", *telemetryInterval)
		log.Printf("doublezero-agent telemetry-storage-path: %s\n", *telemetryStoragePath)
	}

	dzclient, err := agent.GetDzClient(*controllerAddress)
	if err != nil {
		log.Fatalf("Call to GetDzClient failed: %q\n", err)
	}

	// Initialize telemetry collector if enabled
	var telemetryCollector *telemetry.Collector
	if *telemetryEnabled {
		// Validate location pubkey
		if *localLocationPubkey == "" {
			log.Printf("Warning: telemetry enabled but location-pubkey not set, telemetry disabled")
			*telemetryEnabled = false
		} else {
			config := telemetry.CollectorConfig{
				LocalDevicePubkey:         *localDevicePubkey,
				LocalLocationPubkey:       *localLocationPubkey,
				ListenPort:                *telemetryPort,
				SamplingIntervalSeconds:   *telemetryInterval,
				SubmissionIntervalSeconds: *telemetrySubmissionInterval,
				StoragePath:               *telemetryStoragePath,
				MaxSamplesPerLink:         telemetry.DefaultMaxSamplesPerLink,
				EnableInternetProbes:      true,
			}

			// Validate config
			if err := telemetry.ValidateConfig(config); err != nil {
				log.Fatalf("Invalid telemetry configuration: %v", err)
			}

			// Create storage
			storage, err := telemetry.NewFileStorage(config.StoragePath)
			if err != nil {
				log.Fatalf("Failed to create telemetry storage: %v", err)
			}

			// Create collector
			telemetryCollector, err = telemetry.NewCollector(config, storage)
			if err != nil {
				log.Fatalf("Failed to create telemetry collector: %v", err)
			}

			// Start collector
			if err := telemetryCollector.Start(); err != nil {
				log.Fatalf("Failed to start telemetry collector: %v", err)
			}

			log.Printf("Telemetry collector started on UDP port %d", config.ListenPort)

			// Load peers from file if specified
			log.Printf("Telemetry peers file flag: %s", *telemetryPeersFile)
			if *telemetryPeersFile != "" {
				// Try to load peers, but don't fail if file doesn't exist yet
				if _, err := os.Stat(*telemetryPeersFile); err == nil {
					log.Printf("Telemetry peers file exists, attempting to load from %s", *telemetryPeersFile)
					peers, err := loadTelemetryPeers(*telemetryPeersFile)
					if err != nil {
						log.Printf("Warning: failed to load telemetry peers from %s: %v", *telemetryPeersFile, err)
					} else {
						telemetryCollector.UpdatePeers(peers)
						log.Printf("Loaded %d telemetry peers from %s", len(peers), *telemetryPeersFile)
						for i, peer := range peers {
							log.Printf("  Peer %d: %s at %s", i+1, peer.DevicePubkey, peer.IP)
						}
					}
				} else {
					log.Printf("Telemetry peers file %s does not exist yet, will be configured later", *telemetryPeersFile)
				}
			} else {
				log.Printf("No telemetry peers file specified")
			}

			// Ensure clean shutdown
			defer func() {
				if err := telemetryCollector.Stop(); err != nil {
					log.Printf("Error stopping telemetry collector: %v", err)
				}
			}()
		}
	}

	ticker := time.NewTicker(time.Duration(*sleepIntervalInSeconds * float64(time.Second)))

	var eapiClient *agent.EapiClient

	clientConn, err := agent.NewClientConn(*device)
	if err != nil {
		log.Fatalf("call to NewClientConn failed: %v\n", err)
	}

	eapiClient = agent.NewEapiClient(*device, clientConn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := pollControllerAndConfigureDevice(ctx, dzclient, eapiClient, *localDevicePubkey, verbose, *maxLockAge, telemetryCollector)
			if err != nil {
				log.Println("ERROR: pollAndConfigureDevice returned", err)
			}
		}
	}
}
