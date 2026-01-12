package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/state"
	"github.com/spf13/pflag"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	pretty := pflag.Bool("pretty", false, "pretty print JSON output")
	namespace := pflag.String("namespace", "ns-vrf1", "the namespace to get BGP socket stats from (default: ns-vrf1)")
	pflag.Parse()

	ctx := context.Background()
	sockets, err := state.GetBGPSocketStatsInNamespace(ctx, *namespace)
	if err != nil {
		return fmt.Errorf("failed to get BGP socket stats: %w", err)
	}
	fmt.Println(len(sockets))

	var data []byte
	var err2 error
	if *pretty {
		data, err2 = json.MarshalIndent(sockets, "", "  ")
	} else {
		data, err2 = json.Marshal(sockets)
	}
	if err2 != nil {
		return fmt.Errorf("failed to marshal BGP socket stats: %w", err2)
	}
	fmt.Println(string(data))
	return nil
}
