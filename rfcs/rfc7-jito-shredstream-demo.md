# DoubleZero Jito Shredstream Demo

---

## Summary

*A demo for Solana Breakpoint, showing that multicast provides for faster shreds to searchers.*
In order to demonstrate the benefits and market value of the DoubleZero network, we wish to demonstrate that multicast can deliver meaningful improvement to existing Solana software, e.g. Jito Shredstream and its subscribers.

## Motivation

After releasing DoubleZero Mainnet-Beta, it's important to build on the momentum of the project and demonstrate the real value of the network over the public internet.

## New Terminology

*Jito Shredstream Proxy*
This is a proxy client that connects to the Jito Blockengine to forward shreds to subscribers. Traditionally, this has been done using unicast UDP, but now, with DoubleZero, we can use multicast UDP.

## Alternatives Considered

We also considered demonstrating Jito's BAM Client, but, the combination of needing additional features on DoubleZero as well as the belief that BAM is not yet ready for multicast, led us to choose Shredstream. Shredstream is already supported on DoubleZero and will only require small modifications to support the demo.

## Detailed Design

* Instrument Jito Shredstream Proxy to compare unicast vs multicast in a race.
  * Build/deploy shredstream proxy to NYC testnet machine.
  * Connect shredstream proxy to mainnet Jito Blockengine.
  * Attach proxy to DoubleZero testnet and join the shredstream multicast group.
  * Meet with Jito to figure out where in the code to instrument the measurements to compare unicast vs multicast.
  * Long run the experiment and collect data.

## Impact

* Demonstrate a measurable and positive impact of using DoubleZero over the public Internet for Solana applications, particularly Jito Shredstream.
* Develop a better understanding of one class of our users.

## Security Considerations

Any risks associated with running a shredstream proxy.

## Open Questions

* Do we need a client downstream of shredstream proxy to make this work?
