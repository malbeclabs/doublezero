# DoubleZero Controller/Agent

The DoubleZero controller and agent are responsible for the management of DoubleZero devices, including configuration and telemetry collection. 

## Build/Install Instructions

Install Go as a dependency: https://go.dev/doc/install

To build the controller:
```
$ cd controller
$ make build

$ ./bin/controller start -h
Usage of start:
  -listen-addr string
        listening address for controller grpc server (default "localhost")
  -listen-port string
        listening port for controller grpc server (default "443")
  -program-id string
        smartcontract program id to monitor
```

To build the agent:
```
$ cd agent
$ make build

$ ./bin/doublezero-agent -h
Usage of ./bin/doublezero-agent:
  -controller string
        The DoubleZero controller IP address and port to connect to (default "18.116.166.35:7000")
  -controller-timeout-in-seconds float
        How long to wait for a response from the controller before giving up (default 2)
  -device string
        IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543. (default "127.0.0.1:9543")
  -max-lock-age-in-seconds int
        If agent detects a config lock that older than the specified age, it will force unlock. (default 3600)
  -pubkey string
        This device's public key on the doublezero network (default "frtyt4WKYudUpqTsvJzwN6Bd4btYxrkaYNhBNAaUVGWn")
  -sleep-interval-in-seconds float
        How long to sleep in between polls (default 5)
  -verbose
        Enable verbose logging
  -version
        version info
```
