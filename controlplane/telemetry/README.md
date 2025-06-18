# Telemetry Agent

The Telemetry Agent continuously monitors round-trip latency and loss between devices using [TWAMP Light](https://datatracker.ietf.org/doc/html/rfc5357). It periodically discovers peers from the ledger, sends probes, buffers results, and submits telemetry data to an on-chain program.

## Architecture

### Components

- **Collector** – Coordinates the full telemetry pipeline: peer discovery, probing, and submission.
- **PeerDiscovery** – Periodically queries the on-chain serviceability program for devices linked to the local node.
- **Pinger** – Sends TWAMP probes to discovered peers and records RTT/loss.
- **Reflector** – Listens for incoming TWAMP probes from remote devices.
- **Submitter** – Flushes telemetry samples to the on-chain telemetry program.
- **SampleBuffer** – Thread-safe buffer that aggregates telemetry samples in memory.

### System Context Diagram

```mermaid
graph LR
  subgraph Collector
    C[Collector]
    C -->|"runs"| R["TWAMP Reflector"]
    C -->|"runs"| P[Pinger]
    C -->|"runs"| S[Submitter]
    C -->|"runs"| PD[PeerDiscovery]
    C -->|"owns"| SB[SampleBuffer]
    P -->|"record RTT/loss"| SB
    S -->|"flush samples"| SB
  end

  subgraph Ledger
    PD -->|"load devices & links"| PRG["Serviceability Program"]
    S -->|"submit telemetry samples"| TPC["Telemetry Program"]
  end

  subgraph External
    DEV["Remote Devices"]
  end

  DEV -->|"incoming TWAMP probes"| R
  P -->|"outgoing TWAMP probes"| DEV
```

### Sequence Diagram

```mermaid
sequenceDiagram
  autonumber
  participant Collector
  participant PeerDiscovery
  participant Pinger
  participant Reflector
  participant RemoteDevice as Remote Device
  participant Buffer as SampleBuffer
  participant Submitter
  participant TelemetryProgram as Telemetry Program

  Collector->>PeerDiscovery: Start()
  PeerDiscovery->>Serviceability Program: Load()
  Serviceability Program-->>PeerDiscovery: Devices & links
  PeerDiscovery-->>Collector: Peers updated

  Note over Pinger: Probe loop tick

  Pinger->>PeerDiscovery: GetPeers()
  PeerDiscovery-->>Pinger: Map[Peer]
  loop for each Peer
    Pinger->>RemoteDevice: TWAMP Probe
    RemoteDevice-->>Reflector: TWAMP Packet
    Reflector-->>Pinger: TWAMP Response
    Pinger->>Buffer: Add(Sample)
  end

  Note over Submitter: Submission loop tick

  Submitter->>Buffer: CopyAndReset()
  Buffer-->>Submitter: []Sample
  Submitter->>TelemetryProgram: AddSamples(samples)
  TelemetryProgram-->>Submitter: Ack
```

## Configuration

The telemetry agent is configured via command-line flags:

### Required Flags

- `--ledger-rpc-url`: URL of the ledger RPC endpoint.
- `--program-id`: ID of the on-chain telemetry program.
- `--local-device-pubkey`: Public key of the local device.

### TWAMP Settings

- `--twamp-listen-port` (default: `1862`): UDP port to listen for incoming TWAMP probes.
- `--twamp-reflector-timeout` (default: `1s`): Timeout for TWAMP reflector replies.
- `--twamp-sender-timeout` (default: `1s`): Timeout for outgoing TWAMP probes.

### Timing Intervals

- `--probe-interval` (default: `10s`): How often to probe discovered peers.
- `--submission-interval` (default: `60s`): How often to submit collected telemetry.
- `--peers-refresh-interval` (default: `10s`): How often to refresh the peer list from the ledger.

### Logging

- `--verbose`: Enable verbose (debug) logging.
