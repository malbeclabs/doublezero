package agent

import (
	"context"
	"log"
	"slices"
	"time"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DeviceData holds metadata about devices from the controller
// NOTE: placeholder till we get more juicy device info
type DeviceData struct {
	LocalDevice *DeviceInfo
	Peers       []DeviceInfo
}

// DeviceInfo represents information about a device
type DeviceInfo struct {
	DevicePubkey   string
	LocationPubkey string
	PublicIP       string
	LinkPubkey     string
	IsInternetPeer bool
}

func GetConfigFromServer(ctx context.Context, client pb.ControllerClient, localDevicePubkey string, neighborIpMap map[string][]string, controllerTimeoutInSeconds *float64) (config string, err error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*controllerTimeoutInSeconds*float64(time.Second)))
	defer cancel()

	var bgpPeers []string
	bgpPeersByVrf := make(map[string]*pb.BgpPeers)
	for vrf, peers := range neighborIpMap {
		bgpPeersByVrf[vrf] = &pb.BgpPeers{Peers: peers}
		bgpPeers = append(bgpPeers, peers...)
	}
	slices.Sort(bgpPeers)

	req := &pb.ConfigRequest{Pubkey: localDevicePubkey, BgpPeers: bgpPeers, BgpPeersByVrf: bgpPeersByVrf}
	resp, err := client.GetConfig(ctx, req)
	if err != nil {
		log.Printf("Error calling GetConfig: %v\n", err)
		return "", err
	}

	config = resp.GetConfig()

	return config, nil
}

// GetConfigFromServerWithMetadata fetches config and device metadata from the controller
// NOTE: For now, this is a wrapper around GetConfigFromServer that returns placeholder metadata
func GetConfigFromServerWithMetadata(ctx context.Context, client pb.ControllerClient, localDevicePubkey string, neighborIpMap map[string][]string, controllerTimeoutInSeconds *float64) (config string, deviceData *DeviceData, err error) {
	// Get the config using existing method
	config, err = GetConfigFromServer(ctx, client, localDevicePubkey, neighborIpMap, controllerTimeoutInSeconds)
	if err != nil {
		return "", nil, err
	}

	// TODO: Parse device data from controller response once it's enhanced
	// For now, return placeholder data
	deviceData = &DeviceData{
		LocalDevice: &DeviceInfo{
			DevicePubkey: localDevicePubkey,
			// Other fields will be populated when controller is enhanced
		},
		Peers: []DeviceInfo{},
	}

	return config, deviceData, nil
}

func GetDzClient(controllerAddressAndPort string) (pb.ControllerClient, error) {
	conn, err := grpc.NewClient(controllerAddressAndPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	log.Printf("controllerAddressAndPort %s\n", controllerAddressAndPort)
	if err != nil {
		log.Fatalf("GetDzClient call to grpc.NewClient() failed: %v", err)
		return nil, err
	}
	//	defer conn.Close()
	return pb.NewControllerClient(conn), nil
}
