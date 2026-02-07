package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	_ "net/http/pprof"

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
	c.fs.StringVar(&c.listenPort, "listen-port", "8080", "listening port for controller grpc server")
	c.fs.StringVar(&c.env, "env", "", "environment to run controller in (devnet, testnet, mainnet-beta)")
	c.fs.StringVar(&c.programID, "program-id", "", "smartcontract program id to monitor")
	c.fs.StringVar(&c.rpcEndpoint, "solana-rpc-endpoint", "", "override solana rpc endpoint (default: devnet)")
	c.fs.Uint64Var(&c.deviceLocalASN, "device-local-asn", 0, "device local ASN (required when env is not set)")
	c.fs.BoolVar(&c.noHardware, "no-hardware", false, "exclude config commands that will fail when not running on the real hardware")
	c.fs.BoolVar(&c.showVersion, "version", false, "show version information and exit")
	c.fs.StringVar(&c.tlsCertFile, "tls-cert", "", "path to tls cert file")
	c.fs.StringVar(&c.tlsKeyFile, "tls-key", "", "path to tls key file")
	c.fs.BoolVar(&c.enablePprof, "enable-pprof", false, "enable pprof server")
	c.fs.StringVar(&c.tlsListenPort, "tls-listen-port", "", "listening port for controller grpc server")
	return c
}

type ControllerCommand struct {
	fs             *flag.FlagSet
	description    string
	listenAddr     string
	listenPort     string
	env            string
	programID      string
	rpcEndpoint    string
	deviceLocalASN uint64
	noHardware     bool
	showVersion    bool
	tlsCertFile    string
	tlsKeyFile     string
	tlsListenPort  string
	enablePprof    bool
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

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// set build info prometheus metric
	controller.BuildInfo.WithLabelValues(version, commit, date).Set(1)

	// Start pprof server
	if c.enablePprof {
		go func() {
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	// start controller
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	options := []controller.Option{}
	var serviceabilityClient controller.ServiceabilityProgramClient
	var deviceLocalASN uint32

	if c.env == "" {
		if c.programID == "" {
			log.Error("program-id is required when env is not set")
			os.Exit(1)
		}
		if c.rpcEndpoint == "" {
			log.Error("rpc-endpoint is required when env is not set")
			os.Exit(1)
		}
		if c.deviceLocalASN == 0 {
			log.Error("device-local-asn is required when env is not set")
			os.Exit(1)
		}
		serviceabilityClient = serviceability.New(rpc.New(c.rpcEndpoint), solana.MustPublicKeyFromBase58(c.programID))
		deviceLocalASN = uint32(c.deviceLocalASN)
	} else {
		networkConfig, err := config.NetworkConfigForEnv(c.env)
		if err != nil {
			log.Error("failed to get network config", "error", err)
			os.Exit(1)
		}
		serviceabilityClient = serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)
		deviceLocalASN = networkConfig.DeviceLocalASN
	}

	options = append(options, controller.WithDeviceLocalASN(deviceLocalASN))

	if chAddr := os.Getenv("CLICKHOUSE_ADDR"); chAddr != "" {
		chDB := os.Getenv("CLICKHOUSE_DB")
		if chDB == "" {
			chDB = "default"
		}
		chUser := os.Getenv("CLICKHOUSE_USER")
		if chUser == "" {
			chUser = "default"
		}
		chPass := os.Getenv("CLICKHOUSE_PASS")
		chTLSDisabled := os.Getenv("CLICKHOUSE_TLS_DISABLED") == "true"
		cw, err := controller.NewClickhouseWriter(log, chAddr, chDB, chUser, chPass, chTLSDisabled)
		if err != nil {
			log.Warn("clickhouse connection failed, continuing without clickhouse", "addr", chAddr, "error", err)
		} else {
			options = append(options, controller.WithClickhouse(cw))
			log.Info("clickhouse enabled", "addr", chAddr, "db", chDB, "user", chUser, "tls", !chTLSDisabled)
		}
	} else {
		log.Info("clickhouse disabled (CLICKHOUSE_ADDR not set)")
	}

	if c.noHardware {
		options = append(options, controller.WithNoHardware())
	}

	options = append(options, controller.WithServiceabilityProgramClient(serviceabilityClient))

	if c.tlsListenPort != "" {
		options := slices.Clone(options)
		go func(options []controller.Option) {
			log := log.With("mode", "tls")
			options = append(options, controller.WithLogger(log))

			if c.tlsCertFile == "" && c.tlsKeyFile == "" {
				log.Error("tls-cert and tls-key are required when tls-listen-port is provided")
				os.Exit(1)
			}

			cert, err := tls.LoadX509KeyPair(c.tlsCertFile, c.tlsKeyFile)
			if err != nil {
				log.Error("error loading tls cert", "error", err)
				os.Exit(1)
			}
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
				NextProtos:   []string{"h2"},
			}
			options = append(options, controller.WithTLSConfig(tlsConfig))

			addr := net.JoinHostPort(c.listenAddr, c.tlsListenPort)
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				log.Error("failed to listen", "error", err)
				os.Exit(1)
			}
			options = append(options, controller.WithListener(listener))

			server, err := controller.NewController(options...)
			if err != nil {
				log.Error("error creating controller", "error", err)
				os.Exit(1)
			}

			log.Info("starting tls controller", "address", addr)
			if err := server.Run(ctx); err != nil {
				log.Error("runtime error", "error", err)
				os.Exit(1)
			}
		}(options)
	} else {
		if c.tlsCertFile != "" && c.tlsKeyFile != "" {
			cert, err := tls.LoadX509KeyPair(c.tlsCertFile, c.tlsKeyFile)
			if err != nil {
				log.Error("error loading tls cert", "error", err)
				os.Exit(1)
			}
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
				NextProtos:   []string{"h2"},
			}
			options = append(options, controller.WithTLSConfig(tlsConfig))
		}
	}

	log = log.With("mode", "no-tls")
	options = append(options, controller.WithLogger(log))

	lis, err := net.Listen("tcp", net.JoinHostPort(c.listenAddr, c.listenPort))
	if err != nil {
		log.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	options = append(options, controller.WithListener(lis))
	control, err := controller.NewController(options...)
	if err != nil {
		log.Error("error creating controller", "error", err)
		os.Exit(1)
	}

	log.Info("starting controller", "address", net.JoinHostPort(c.listenAddr, c.listenPort))
	if err := control.Run(ctx); err != nil {
		log.Error("runtime error", "error", err)
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
