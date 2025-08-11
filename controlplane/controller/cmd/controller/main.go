package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/controller/internal/controller"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

var (
	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type Runner interface {
	Init([]string) error
	Run() error
	Name() string
	Fs() *flag.FlagSet
	Description() string
}

func NewAgentCommand() *AgentCommand {
	a := &AgentCommand{
		fs:          flag.NewFlagSet("agent", flag.ExitOnError),
		description: "command set for interacting with controller",
	}
	a.fs.StringVar(&a.pubkey, "device-pubkey", "", "pubkey of device which to fetch config")
	a.fs.StringVar(&a.controllerAddr, "controller-addr", "localhost", "listening address of controller")
	a.fs.StringVar(&a.controllerPort, "controller-port", "443", "listening port of controller")
	a.fs.StringVar(&a.unknownPeers, "unknown-peers", "", "comma separated list of unknown peers to remove from config")
	return a
}

type AgentCommand struct {
	fs             *flag.FlagSet
	description    string
	pubkey         string
	controllerAddr string
	controllerPort string
	unknownPeers   string
}

func (a *AgentCommand) Fs() *flag.FlagSet {
	return a.fs
}

func (a *AgentCommand) Name() string {
	return a.fs.Name()
}

func (a *AgentCommand) Description() string {
	return a.description
}

func (a *AgentCommand) Init(args []string) error {
	return a.fs.Parse(args)
}

func (a *AgentCommand) Run() error {
	target := net.JoinHostPort(a.controllerAddr, a.controllerPort)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		slog.Error("error creating controller client", "error", err)
		os.Exit(1)
	}
	defer conn.Close()
	defer cancel()

	unknownPeers := []string{}
	if a.unknownPeers != "" {
		unknownPeers = append(unknownPeers, strings.Split(a.unknownPeers, ",")...)
	}
	agent := pb.NewControllerClient(conn)
	got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: a.pubkey, BgpPeers: unknownPeers})
	if err != nil {
		slog.Error("error while fetching config", "error", err)
		os.Exit(1)
	}
	fmt.Println(got.Config)
	return nil
}

func NewControllerCommand() *ControllerCommand {
	c := &ControllerCommand{
		fs:          flag.NewFlagSet("start", flag.ExitOnError),
		description: "command set for starting controller",
	}
	c.fs.StringVar(&c.listenAddr, "listen-addr", "localhost", "listening address for controller grpc server")
	c.fs.StringVar(&c.listenPort, "listen-port", "443", "listening port for controller grpc server")
	c.fs.StringVar(&c.env, "env", "", "environment to run controller in (devnet, testnet, mainnet)")
	c.fs.StringVar(&c.programID, "program-id", "", "smartcontract program id to monitor")
	c.fs.StringVar(&c.rpcEndpoint, "solana-rpc-endpoint", "", "override solana rpc endpoint (default: devnet)")
	c.fs.BoolVar(&c.noHardware, "no-hardware", false, "exclude config commands that will fail when not running on the real hardware")
	c.fs.BoolVar(&c.showVersion, "version", false, "show version information and exit")
	return c
}

type ControllerCommand struct {
	fs          *flag.FlagSet
	description string
	listenAddr  string
	listenPort  string
	env         string
	programID   string
	rpcEndpoint string
	noHardware  bool
	showVersion bool
}

func (c *ControllerCommand) Fs() *flag.FlagSet {
	return c.fs
}

func (c *ControllerCommand) Name() string {
	return c.fs.Name()
}

func (c *ControllerCommand) Description() string {
	return c.description
}

func (c *ControllerCommand) Init(args []string) error {
	return c.fs.Parse(args)
}

func (c *ControllerCommand) Run() error {
	if c.showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	// set build info prometheus metric
	controller.BuildInfo.WithLabelValues(version, commit, date).Set(1)

	// start controller
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	options := []controller.Option{}
	var serviceabilityClient controller.ServiceabilityProgramClient
	if c.env == "" {
		if c.programID == "" {
			slog.Error("program-id is required")
			os.Exit(1)
		}
		if c.rpcEndpoint == "" {
			slog.Error("rpc-endpoint is required")
			os.Exit(1)
		}
		serviceabilityClient = serviceability.New(rpc.New(c.rpcEndpoint), solana.MustPublicKeyFromBase58(c.programID))
	} else {
		networkConfig, err := config.NetworkConfigForEnv(c.env)
		if err != nil {
			slog.Error("failed to get network config", "error", err)
			os.Exit(1)
		}
		serviceabilityClient = serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)
	}

	if c.noHardware {
		options = append(options, controller.WithNoHardware())
	}

	lis, err := net.Listen("tcp", net.JoinHostPort(c.listenAddr, c.listenPort))
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	options = append(options, controller.WithListener(lis))
	options = append(options, controller.WithServiceabilityProgramClient(serviceabilityClient))
	control, err := controller.NewController(options...)
	if err != nil {
		slog.Error("error creating controller", "error", err)
		os.Exit(1)
	}

	slog.Log(ctx, slog.LevelInfo, fmt.Sprintf("starting controller on %s", net.JoinHostPort(c.listenAddr, c.listenPort)))
	if err := control.Run(ctx); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
	return nil
}

func root(args []string) error {
	cmds := []Runner{
		NewAgentCommand(),
		NewControllerCommand(),
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "\nUsage:\n\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 0, 3, ' ', 0)
		for _, cmd := range cmds {
			fmt.Fprintf(w, "\t%s\t%s\t\n", cmd.Name(), cmd.Description())
		}
		w.Flush()
	}

	if len(args) < 1 {
		return errors.New("error: you must pass a sub-command")
	}

	subcommand := os.Args[1]

	for _, cmd := range cmds {
		if cmd.Name() == subcommand {
			cmd.Init(os.Args[2:]) // nolint:all
			return cmd.Run()
		}
	}

	return fmt.Errorf("unknown subcommand: %s", subcommand)
}

func main() {

	opts := &slog.HandlerOptions{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	if err := root(os.Args[1:]); err != nil {
		fmt.Println(err)
		flag.Usage()
		os.Exit(1)
	}
	flag.Parse()
}
