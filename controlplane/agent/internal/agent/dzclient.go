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
