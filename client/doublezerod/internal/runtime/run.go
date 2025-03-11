package runtime

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
	"golang.org/x/sys/unix"
)

func Run(ctx context.Context, sockFile string, enableLatencyProbing bool, programId string, rpcEndpoint string) error {
	nlr := netlink.Netlink{}
	bgp, err := bgp.NewBgpServer(net.IPv4(1, 1, 1, 1))
	if err != nil {
		return fmt.Errorf("error creating bgp server: %v", err)
	}

	db, err := netlink.NewDb()
	if err != nil {
		return fmt.Errorf("error initializing db: %v", err)
	}

	nlm := netlink.NewNetlinkManager(nlr, bgp, db)

	errCh := make(chan error)

	// starting network manager will attempt to recover latest provisioned state
	log.Println("network: starting network manager")
	go func() {
		err := nlm.Serve(ctx)
		errCh <- err
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /provision", nlm.ServeProvision)
	mux.HandleFunc("POST /remove", nlm.ServeRemove)
	mux.HandleFunc("GET /status", nlm.ServeStatus)

	if enableLatencyProbing {
		latency := latency.NewLatencyManager(latency.FetchContractData, latency.UdpPing)
		go func() {
			err := latency.Start(ctx, programId, rpcEndpoint)
			errCh <- err
		}()
		mux.HandleFunc("GET /latency", latency.ServeLatency)
	}

	opts := []api.Option{
		api.WithBaseContext(ctx),
		api.WithHandler(mux),
	}
	if sockFile != "" {
		opts = append(opts, api.WithSockFile(sockFile))
	}

	api := api.NewApiServer(opts...)

	lis, err := net.Listen("unix", sockFile)
	if err != nil {
		return fmt.Errorf("error creating listener: %v", err)
	}
	defer unix.Unlink(sockFile) //nolint

	err = os.Chmod(sockFile, 0666)
	if err != nil {
		log.Printf("error setting socket file perms: %v", err)
	}

	log.Println("http: starting api manager")
	go func() {
		err := api.Serve(lis)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		log.Println("teardown: cleaning up and closing")
		nlm.Close()
		api.Close()
		return nil
	case err := <-errCh:
		return err
	}
}
