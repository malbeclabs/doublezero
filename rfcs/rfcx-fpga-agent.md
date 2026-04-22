# Doublezero FPGA Agent & FPGA Connections

## Summary

**Status: Draft**

The FPGAs providing the "outer ring" of DoubleZero will need to be loaded with bitstreams and have configuration data managed by software. This RFC documents how the FPGAs will be connected to the Network Switches that they are associated with, as well as what software will be required to manage the FPGA (The FPGA Agent), including loading FPGA binaries and configuration information, as well as reporting metrics and other status back to the Ledger and metrics infrastructure. This is required for the FPGAs to provide mutable edge filtering services.

<br>

## Motivation

Why now: FPGA hardware should be available for installation during early Q2 of 2026. There needs to be connection instructions and software to manage installation of FPGAs in place for contributors to use when they receive FPGA hardware.

<br>

## New Terminology

__Base Image:__ This is an FPGA programming file that is installed into flash memory on the FPGA card.

__Run Image:__ This is an FPGA programming file that is loaded after power is applied, and is not persistent through power cycles or restarts.



## Alternatives Considered

- FPGA Agent as part of a current agent. This would result in fewer separate Agent softwares running on DZDs, but would bundle unrelated functions together. For example, transit DZDs do not have FPGAs associated with them, and do not need the FPGA Agent functionality.
- FPGA Agent running inside FPGAs (self-managed FPGAs). This allows the FPGAs to be a self contained system with no outside dependencies. However this reduces recoverability since a bad software update could brick FPGAs, and means that DoubleZero has persistent software installed in more places. This would also require the FPGAs to be more than just a bump in the wire, since they would be sending and receiving their own data. A preferred solution would have contributors only installing and managing software in a single place.
- FPGAs with persistent images that only are updated during software updates. This is not preferred because of the chance for a stale image to be unexpectedly present. In that event it would take 5+ minutes per FPGA for a new image to be programmed if the agent discovers a stale image, rather than ~40 seconds.

<br>

## Detailed Design

### Architecture

At present, the DoubleZero system will have two FPGAs that are in a separate chassis (DZCH20). Both of the FPGAs and the chassis microcontroller are connected to the DZD (Arista 7280) via a USB connection. The USB connection provides UART control for the µC and UART & JTAG control for the FPGAs.

The two FPGAs will come factory-programmed with identical passthrough images in Flash memory that also can read which slot the FPGA is installed in. This is what they will come up running when power is applied.

The FPGA agent has three distinct tasks to perform:
1. Powering on the FPGAs via the chassis µC, identifying which FPGA is in which slot, and loading the appropriate run images into them.
2. Loading any configuration data into the FPGAs (for example, information about what IPs have subscribed to what filtering services)
3. Monitoring FPGA statistics and health, reporting metrics, and automatically trying to recover from any FPGA failures. 

### Physical Hardware & Connections
Two FPGAs are required to be wired in series, plus a loopback cable to the network switch. Further details about routing are available in RFC## that describes the routing for edge filtering.
```
  ┌─────────────────────────────────────────────────────────────────────────────────┐ 
  │                                      DZCH20                                 USB │<─┐
  │ ┌─────────────────────────────────────┐ ┌─────────────────────────────────────┐ │  │
  │ │            V80 Card Left            │ │            V80 Card Right           │ │  │
  │ │                                     │ │                                     │ │  │
  │ │ [Port 1] [Port 2] [Port 3] [Port 4] │ │ [Port 1] [Port 2] [Port 3] [Port 4] │ │  │
  │ └─────────────────────────────────────┘ └─────────────────────────────────────┘ │  │
  └────── ^ ────── ^ ──────────────────────────── ^ ────── ^ ───────────────────────┘  │
          │        └──────────────────────────────┘        │                           │
          │        ┌──────── FPGA Bypass ─────────┐        │                           │
          v        v         (Loopback)           v        v                           │
    ┌─────────────────────────────────────────────────────────────────────────────┐    │
    │   [Etha]   [Ethb]                         [Ethy]   [Ethz]                   │    │
    │    Outer Ring                               Inner Ring (filtered vrf)       │    │
    │                        Network Switch (Arista 7280)                     USB │────┘
    └─────────────────────────────────────────────────────────────────────────────┘
```

### Pre-Requisite Software
Contributors will be required to acquire the Vivado Labs 2024.2 installer. This is freely available, but may require creating an account with AMD. This includes the cable drivers and xsdb software required to communicate with the V80 FPGAs over USB.

### Installation
FPGA Agent Software, Vivado Labs, and cable drivers will be installed by a single rpm. The installer will require the Vivado Labs installer tarball to be located at `/mnt/flash/fpga/Vivado_Lab_Lin_2024.2*.tar`.

Initial versions of the installer will include FPGA binaries, but an evolution of this should be for FPGA binaries to be fetched periodically, so that FPGA bug fixes can be distributed without requiring contributor action.

Vivado Labs will be installed at: `/mnt/flash/fpga/Xilinx/`
> 💡 Any time a vivado component is run, `/mnt/flash/fpga/Xilinx/Vivado_Lab/2024.2/bin/` will need to be in the `$PATH`.
FPGA binaries will be placed at:  `/mnt/flash/fpga/images`
FPGA Helper TCL scripts will be placed at: `/mnt/flash/fpga/helpers`

__Arista 7130:__ While 7130s aren't going to be supported for production edge filtering, we will be testing in testnet and devnet with them. To that end, the installer's post install script wil check `cli show version`, and not install cable drivers after installing vivado_labs. Instead 7130 operators will need to manually install and configure Arista's `jtag-1.1.0-1.4.swix`.

### Agent Software

#### Agent Configuration
The Agent software will be passed a few runtime parameters on startup. These include ones that the `doublezero_agent` gets, like `--controller`, `--pubkey`, `--sleep-interval-in-seconds`, `--metrics-enable` and `--metrics-addr`, and also some FPGA specific ones:
- `--7130` indicates Arista 7130. 7130 support is anticipated only for simple passthrough and testnet testing use. This flag implies the `--noch` flag as well.
- `--noch` Indicates that the DoubleZero Chassis DZCH20 is not in use.
- `--fpga0` and `--fpga1` FPGA Image type for each FPGA (Left/Right). Potential arguments are:
  * default
  * passthrough
  * sigv
  * dedup
  * fpna

The FPGA type (xcv80, xcvu9p, other) will be dynamically discovered.

#### Startup Behavior

- The Agent will open a serial connection and verify the presence of the Chassis
- The Agent will start XSDB in the background from Vivado Labs for JTAG communication with the FPGAs
- The Agent software will call into xsdb to see what FPGAs are present and group the various JTAG/XSDB endpoints together into coherent entities
- The Agent will then query each FPGA to determine left/right slot
- The Agent will load the correct binary into each FPGA as an ephemeral run image. <br>
  _This takes ~40s, but is substantially faster than the 5+m for flash programming_
- The Agent will start monitoring processes

#### Monitoring Behavior
The FPGA Agent will handle monitoring for
1. Metrics for Left FPGA's image
2. Metrics for Right FPGA's image
3. Power usage and Chassis fans

and report the data via Prometheus so alerting can be raised in the event of an issue. Since this data is for error reporting and monitoring, not rewards calculations, it does not need to be published on-chain.

_Potential Automated Behaviors:_ The agent has the potential to automatically try and recover the FPGAs in the event of behaviors that it sees as anomalous during monitoring. Some examples of this could be:
1. Arista I/O bytes =/= FPGA I/O bytes- FPGA may have deadlocked: re-load images
2. FPGA power draws rapidly increase or decrease: power cycle FPGA cards to see if the issue resolves, and eventually just turn them off if the error persists.

#### Control Behavior
The FPGA Agent will also provide control configuration to the FPGAs. This may include things like providing IP addresses for Validators subscribed to packet filtering, along with configuration options requested by those validators.

Control data will be sourced from the DZ ledger via the controller, polled at the interval provided by `--sleep-interval-in-seconds`. When users request a connection in edge filtering mode, they will also provide all required configuration for the FPGA, including what type of filtering they're opting into.

#### User interaction
When a user connects for edge filtering, they will need to specify what kind of edge filtering they want. Depending on the category of filtering, there may be more options they can specify.

As an example of a potential configuration option. When subscribing to the Solana Edge Filtering, a user can choose whether the FPGA should Drop or Allow TPU traffic that the FPGA cannot decrypt or parse. An example of each connection option:
`doublezero connect edgefiltering solana drop-unknown-tpu`
`doublezero connect edgefiltering solana accept-unknown-tpu`

Additional options will likely emerge during development of FPGA features.


#### User Struct
A new item will be added to the User struct to capture Edge filtration settings, and the CLI, serviceability, and controller will need to be updated to handle it.
``` rust
pub struct User {
  pub account_type: AccountType, // 1
  //...
  pub filter_opts: EdgeFilterOptions,
}

pub struct EdgeFilterOptions {
  pub filter_type: EdgeFilterTypes,
  pub drop_no_keys: bool,
  // More options to come, e.g. FPNA program ID
}

#[repr(u8)]
pub enum EdgeFilterTypes {
  #[default]
  None = 0,
  Solana = 1, // Example Type: Subscribe to TPU Path Filtering
  ShredSubscriber = 2, // Example Type: Subscribe to Deduped Raw Shreds
  TransactionSubscriber = 3, // Example Type: Subscribe to Transaction Feed
}
```

### Controller
The controller will need to recognize edge filtering users, and respond to poll calls from the FPGA Agent, in addition to the usual information collected by doublezero_agent. Specifically the FPGA agent will need to receive the `filter_opts` and `dz_ip`.

<br>

## Impact

This RFC requires contributors to acquire Vivado Labs from AMD. It will add a new software agent daemon running on the Arista DZDs, and a new background process `xsdb` from AMD running as well. 

<br>

## Security Considerations

Bad configuration coming from the controller could potentially cause FPGAs to drop legitimate traffic or allow malicious traffic. The agent should validate configuration data received from the controller before applying it to the FPGAs. Additionally, the FPGA binaries themselves should be cryptographically signed and verified before loading to prevent malicious bitstream injection.

<br>

## Backward Compatibility

No anticipated issues:
- Existing DZDs without FPGAs will see no changes. These DZDs will not support edge filtering users connecting.
- New Agent means old agents can continue to be FPGA-agnostic.

<br>

## Open Questions
None
