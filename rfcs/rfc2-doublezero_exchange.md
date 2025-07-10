# DoubleZero Exchange - DZX

## Summary

The DoubleZero Exchange (DZX) is a network architectural component designed to ensure a contiguous global DZ network between multiple contributor nodes (DZDs).  The scope of a DZX is a single geographic region, typically a metro or city. Its purpose is to scale the DZ network by enabling broader reach across data centers and contributors.

As the number of DZDs in a metro grows, typically beyond three, the complexity and cost of full-mesh or direct interconnects increase exponentially. The DZX addresses this through the introduction of a fabric-based design, analogous to a hub-and-spoke topology, where contributors connect to a shared local exchange, simplifying deployment through reduced fiber requirements and improving operational manageability.  A DZX should be a solution operated by the DZ community.

In the long term, the DZX aligns with the DZ project’s strategic vision of permissionless networking by serving as a physical access point to enable future contributors to join the network. DZD connections should be governed and validated by smart contracts, automated provisioning, verification and ongoing compliance.

### Design considerations
A successful design:
- Enables a globally contiguous DZ network
- Expands decentralization
  - Multiple DZX operators
  - Multiple data-center vendors
- Captures intent with a smart-contract
- Automates logical components
- Enhances the vision of permissionless networking

Note that [DZX Economics](https://www.notion.so/DZX-Economics-2088eaaee713801f95aaf84a5003314c?source=copy_link) provides detail on the economic model and potential incentives for operating a DZX.  This document does not consider the pros or cons of single or dual-ownership.

## Motivation

The DZX is required as the ecosystem expands beyond three DZDs in specific metros.  By Q3 2025 we had identified this requirement in multiple major metro areas such as New York City, London, Frankfurt and others.

Consider the evolution of DZ as it expands into new cities and metros:
- Single DZD, single contributor
  - Metro is most centralized, single point of failure
  - No DZX required
- Two DZDs, single data center, single contributor
  - Contributor may have deployed redundant DZDs in their data center
  - Metro is highly centralized, contributor is single point of failure, increased resiliency in metro network
  - Switches are interconnected, most likely with a simple intra rack or intra cage cabling
  - Negligible cost, low latency, low complexity
- Two DZDs, single data center, multiple contributors
  - Decentralization across contributors, resiliency in network, centralized within a single data center vendor
  - Switches are interconnected using point-to-point cross-connects
  - Low cost, low latency, low complexity
- Multiple DZDs, multiple data centers and vendors, multiple contributors
  - Decentralization across contributors, data centers and vendor, highly resilient network
  - Switches are interconnected using a mixture of hub and spoke and/or point-to-point interconnects
  - High cost, acceptable latency, higher complexity, greatest scale

The motivation for the DZX is to allow DZ to scale to maximize reach and decentralization while still supporting DZ's primary goal of IBRL.

## New Terminology

- DoubleZero Exchange (DZX): a network fabric creating a contiguous network between all network contributors with a metro
- DoubleZero Device / DoubleZero eXchange (DZDx): a single DZ network device that acts as both a DZD and a DZX
- DZX Operator: organization that owns the DZX network equipment
- Metro: the geographical area where DZDs are deployed and interconnected together across facilities within a city or urban area.

## Alternatives Considered

- Do nothing: 
  - Advantages:
    - None
  - Disadvantages:
    - DZ network is severely limited in its growth without being able to interconnect multiple contributors beyond point-to-point cross-connects
- Leverage public Internet by connecting DZDs via GRE over Internet:
  - Advantages:
    - Reuse existing investment, i.e. cheap
  - Disadvantages:
    - Unable to guarantee MTU
    - Loose end-to-end visibility
    - Potential additional capacity required for Internet connections

## Detailed Design

### High-Level Physical Topology Options

<img src="rfc2/images/DZX-Topologies.png" alt="DZX Physical Topologies" width="1700" height="450"/>

Figure 1: DZX Topology Options

| Option     | Description | Pros | Cons | Recommendation |
|------------|-------------|------|------|----------------|
| **Option 1** | - Simplest DZX setup<br>- Single switch requiring links from each contributor in the metro<br>  - Links may be:<br>    - Cross-connects within a campus<br>    - Tail circuits across cities | - Simple<br>- Cheap<br>- Quickest time to market | - Highly centralized<br>- Single point of failure<br>- Scalability limited to at most the number of ports on the switch (see section below) |
| **Option 2** | - Redundant DZX switches operated by multiple Operators across two strategic data centers<br>- Requires only one DZX connection per DZD<br>- Requires interconnect between DZX switches | - Increased decentralization<br>- No single point of failure<br>- Contributors can choose their DZX Operator | - Risk of network discontinuity if interconnect fails or an Operator stops participating<br>- Scalability limited to ~2× switch port count |
| **Option 3** | - Redundant DZX switches operated by multiple Operators across two strategic data centers<br>- Each contributor must purchase links to both DZXs<br>- No interdependence between DZX Operators | - Most decentralized<br>- Highest redundancy | - Most expensive for Network Contributors |
| **Option 4** | - Redundant DZX switches operated by multiple Operators across two strategic data centers<br>- Each contributor must purchase at least one link to a DZX<br>  | - Highly decentralized<br>-  Highly redundant <br>- Optionality for Network Contributors | - Risk of network discontinuity if interconnect fails or an Operator stops participating <br>- Scalability limited to ~2× switch port count | ⭐ <br>- Sweet spot that balances cost to contributors, redundancy and decentralization 

### 7280CR3A port speed options: 

<img src="rfc2/images/DZX-Arista7280-PortOptions.png" alt="DZX Option 1 Arista 7280 Port Options" style="max-width: 100%;" />

[Arista 7280 Datasheet](https://www.arista.com/assets/data/pdf/Datasheets/7280R3A-Datasheet.pdf) 

### DZX Logical Topology

<img src="rfc2/images/DZX-Logical-Option1.png" alt="DZX Logical" width="400" height="400"/>
<img src="rfc2/images/DZX-Logical-Option2.png" alt="DZX Logical" width="400" height="400"/>
<img src="rfc2/images/DZX-Logical-Option3.png" alt="DZX Logical" width="400" height="400"/>
<img src="rfc2/images/DZX-Logical-Option4.png" alt="DZX Logical" width="400" height="400"/>

Figure 2: DZX Logical Layer 2

| Option                           | Description                                                                                                                                                                                                                                                                                                                                                                                                                        | Pros                                                                                                                                                                                                                       | Cons                                                                                                                                                                                                                                                                                                                        | Recommendation                                                                                                                                                                    |
|----------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **1. DZX as a Layer 2 Fabric**   | - DZX operated by DZ community members<br>- Each DZX switch operates at layer 2<br>- A single subnet (broadcast domain) is used to address all DZD NNIs facing the DZX<br>- The existing DZ network routing is extended across the DZX<br>&nbsp;&nbsp;&nbsp;- A full-mesh of IS-IS/PIM neighbors is formed across the DZX<br>&nbsp;&nbsp;&nbsp;- The DZX is transparent from a routing perspective<br>- All links are assumed to be of equal latency<br>&nbsp;&nbsp;&nbsp;- Helps incentivize diversification out of a single data center<br>&nbsp;&nbsp;&nbsp;- Prevents an arms race in the metro<br>- The DZX switch maintains a layer 3 connection for telemetry | - The DZX switch requires a simpler feature set (layer 2 only) than a DZD<br>- Allows the DZX operator to purchase less expensive switch platforms e.g. Arista 7060X6 Tomahawk<br>- All links are assumed to be of equal latency<br>&nbsp;&nbsp;&nbsp;- Helps incentivize diversification out of a single data center<br>&nbsp;&nbsp;&nbsp;- Prevents an arms race in the metro<br>- Full visibility for monitoring and troubleshooting | - New automation and configuration model to support DZX layer 2 switch use-case<br>- Small switch buffers with Tomahawk may not be sufficient to absorb traffic bursts towards busy DZDs in the metro<br>&nbsp;&nbsp;&nbsp;- Would likely require traffic shaping on DZDs<br>- Limitations of scaling a single broadcast domain, for example the DZX topology MUST remain loop-free and MUST NOT rely on spanning-tree protocols<br>&nbsp;&nbsp;&nbsp;- Limited to two DZX switches<br>&nbsp;&nbsp;&nbsp;- Requires uses of layer 2 protocols such as spanning-tree protocol, storm-control<br>- Complex to integrate within a hybrid community/vendor model<br>&nbsp;&nbsp;&nbsp;- Layer 2 hand-off complexity e.g. mapping between VLANs |                                                                                                                                                                                    |
| **2. DZX as a Layer 3 Fabric**   | - DZX operated by DZ community members<br>- Each DZX switch operates at layer 3<br>- Allows a DZD to become a DZDx<br>&nbsp;&nbsp;&nbsp;- The DZD terminates DZX links from multiple contributors originating within the same metro<br>- A layer 3 connection (single subnet) is built between DZD to DZX/DZDx per NNI<br>- The existing DZ network routing is extended to the DZX/DZDx<br>&nbsp;&nbsp;&nbsp;- IS-IS/PIM neighbors are established between each DZD and/or DZDx<br>- The DZX/DZDx fully participates in routing<br>- All links are assumed to be of equal latency<br>&nbsp;&nbsp;&nbsp;- Helps incentivize diversification out of a single data center<br>&nbsp;&nbsp;&nbsp;- Prevents an arms race in the metro | - DZX has equivalent functionality to DZD, either as a dedicated device or as a DZDx<br>- By participating in routing, in future it can support end-to-end traffic-engineering planning<br>- Requires minimal additional development support in the controller/configuration templates<br>- Full visibility for monitoring and troubleshooting<br>- Likely simplifies integration of any potential hybrid community/vendor model<br>&nbsp;&nbsp;&nbsp;- Simple routed hand-offs to vendors | - More expensive switches if using dedicated layer 3 devices rather than DZDx i.e. Jericho2/7280CR3                                                                                                                                                                                                                 | ⭐<br>- Results in the most visibility into, and control of, end-to-end traffic flows<br>- Creation of DZDx allows reuse of existing hardware with nominal further capital expense<br>&nbsp;&nbsp;&nbsp;- Optics and cabling<br>&nbsp;&nbsp;&nbsp;- DZDx option also has relatively fast time-to-market |
| **3. DZX as a Vendor Fabric**    | - Various commercial solutions exist to create last mile connectivity similar to the DZX, e.g.:<br>&nbsp;&nbsp;&nbsp;- Packetfabric<br>&nbsp;&nbsp;&nbsp;- MegaPort<br>&nbsp;&nbsp;&nbsp;- Equinix Fabric<br>- Likely facilitates logical cross-connects between DZDs e.g. Virtual Cross-Connects (VXC)<br>- Access to immediate scale, fastest time-to-market<br>- All links are assumed to be of equal latency<br>&nbsp;&nbsp;&nbsp;- Helps incentivize diversification out of a single data center<br>&nbsp;&nbsp;&nbsp;- Prevents an arms race in the metro | - Vendor solution abstracts away operational overhead of operating DZX for a cost<br>- Fastest time-to-market<br>- Easiest to scale across multiple metros<br>- Automated provisioning already developed by vendor, including LOA management | - Lack of visibility into network fabric<br>- Dependency on Web2 businesses for supporting Web3 project e.g. MSAs<br>- Assumed to be most expensive                                                                                                                                             |                                                                                                                                                                                    |
| **4. Hybrid Models**             | - Combine DZX community and vendor solution<br>- DZX community layer 2 and layer 3 options are mutually exclusive<br>- Interconnect DZX switches with vendor network fabrics<br>- All links are assumed to be of equal latency<br>&nbsp;&nbsp;&nbsp;- Helps incentivize diversification out of a single data center<br>&nbsp;&nbsp;&nbsp;- Prevents an arms race in the metro | - Combines the best of Web3 decentralization with scale of existing Web2 offering | - Lack of visibility into network fabric<br>- Significant increase in complexity supporting different modes of operation                                                                                                                                                             |                                                                                                                                                                                    |
 
## Impact

- Fundamentally affects the ability of DZ to deliver its core product
- Ensures that DZ performance - latency, bandwidth and jitter - are optimized end-to-end
- Allows scaling to many network contributors, realizing decentralization efforts

## Security Considerations

- No new security threats or attack vectors
- A fully decentralized DZX helps mitigate the risk of censorship by any single organization

## Backwards Compatibility

- Not applicable

## Open Questions
- Need to understand if there are any unknown implications that results from converting a DZD to a DZDx
  - Example: in NYC, an existing LON long-haul link on the DZDx become favored as it is more central to the network graph than the equivalent LON link terminated on a DZD
- Find out pricing difference between DZX switches if used as layer 2 vs layer 3
- Commercially available network fabrics from vendors:
  - Technical capabilities
    - Once known, a separate RFC or design must be created before deployment
  - Pricing
- Any additional telemetry features required for DZX links?
  - Does telemetry need awareness that there are multiple owners of a single link?
