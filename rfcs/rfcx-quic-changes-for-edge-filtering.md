# DoubleZero QUIC Changes for Edge Filtering

## Summary

**Status: Draft**

Some modifications to QUIC will be required to allow an FPGA in the DoubleZero network to provide edge filtering services for Solana validators. The overarching goal is to make minimal changes to Solana Validator and QUIC library code, as compared to Version 1 of QUIC currently used. Additionally, as much as possible to make changes in such a way that that client-side (such as RPCs) code requires little to no change, or changes can be phased in. This RFC proposes making changes or restrictions in three areas of QUIC: Encryption, Flow Control, and Packet Formatting.

<br>

## Motivation

Edge Filtration is one of the key things that DoubleZero is uniquely positioned to provide for Solana. The DoubleZero network architecture allows expensive FPGAs to provide filtering services for multiple validators at once, effectively sharing the cost of these high-priced and flexible components. In the time since the proof-of-concepts for the edge filtering were done, Solana transitioned from using straight UDP to QUIC for its TPU connections. QUIC transactions now make up >95% of Solana TPU traffic, so any effective edge filtering must be able to filter QUIC traffic. There are three distinct challenges that are imposed by QUIC as compared to UDP.

1. Encryption
    
    By design QUIC encrypts as much of the packet and header as possible to prevent MitM attacks and network snooping. The FPGA cannot parse encrypted data without the decryption keys.
    
2. Flow Control

    QUIC connections are "reliable" like TCP in such a way that dropped packets would cause the sender to continuously re-send dropped data. Even if that is worked-around by faking an ACK of dropped packet back towards the sender, the sender would also eventually use up all their data credits (reaching the `MAX_DATA` limit provided by the server) and stall the connection. The server only advances the `MAX_DATA` allowance once an application has retrieved the data from the protocol layer. If the data never arrives, the server will not increase `MAX_DATA` and the client will not be able to send more data.
    
3. Packet Formatting
    
    The structure and contents of packets are incredibly variable, and a parser must understand every previous frame in a packet to process the next frame. This makes them difficult to parse in an FPGA. Additionally, stream data can be fragmented arbitrarily, making it hard to analyze the stream data.

<br>

## New Terminology

- **Client** - Transaction Sender (e.g. RPC)
- **Server** - Validator 
<br>

## Alternatives Considered

- Only filter UDP TPU traffic
- Create a new version of QUIC that is more adapted for filtering
- Build an all-new protocol to use for TPU traffic
- Don't provide filtering services that requires deep packet inspection
- Provide deep packet inspection, but close connections as the filtering method

<br>

## Scope

This RFC does not consider _how_ Wiredancer will process packets. The scope of this is how to make QUIC packets understandable at all. One way to think about the scope is: This RFC covers what would be required for the FPGA to be able to drop all streams with even numbers.

<br>

## Detailed Design

### Architecture

```
                   ┌─────────────────────────────────────────────────────────────────┐
                   │                                FPGA                             │
                   │                                                                 │
                   │     ┌────────────────────────────────────────┐                  │
┌──────────────┐   │     │                QUIC PARSER             │                  │
│              │   │     │     ┌─────────────┐                    │                  │
│ RPC / Client │───│───┬─│────>|   Decrypt   │––– have keys ––––––│––––––––┐         │
│              │<──│─┬─│─│───┐ |             |––– no keys –┐      │        │         │
└──────────────┘   │ │ │ │   │ └─────────────┘             │      │        │         │
                   │ │ │ │   │         ^                   │      │        │         │
                   │ │ ↓ │   │         │                   │      │        │         │
                   │  N  │   │    ┌────┴──────┐            │      │        │         │
                   │  o  │   │    │ Key Store │––––┐       │      │ ┌──────────────┐ │
                   │  t  │   │    └───────────┘    │       │      │ │              │ │
                   │     │   │             ^       │       │      │ │              │ │
                   │  Q  │   └ Data ┐     Keys     │       │      │ │  WireDancer  │ │
                   │  U  │          |      |       │       │      │ │ (Dedup/Sigv) │ │
                   │  I  │   ┌──────────────────┐  │       │      │ │              │ │
                   │  C  │   │ Key Extract from │  │       │      │ │              │ │
                   │ ↑ │ │   │ HANDSHAKE_DONE   │  │       │      │ │              │ │
                   │ │ │ │   └──────────────────┘  │       │      │ │   Drop Pass  │ │
                   │ │ │ │          ^              │       │      │ └──────────────┘ │
                   │ │ │ │ ┌────────┘      ┌───────┘       │      │      │    │      │
┌──────────────┐   │ │ │ │ │  ┌─────────── V ──────────────┘      │      │    │      │
│  Validator / │───│─┴─│─│─┘  │ ┌────────────┐    ┌────────────┐  │      │    │      │
│    Server    │<––│───┴─│────┴─│ Re-Encrypt │<─┬─│ Frame Swap │<─│──────┘    │      │
└──────────────┘   │     │      └────────────┘  | └────────────┘  │           │      │
                   │     │                      |                 │           │      │
                   │     │                      └–– Approve ──────│───────────┘      │
                   │     └───────────────────────────────────────–┘                  │
                   │                                                                 │
                   │                                                                 │
                   └─────────────────────────────────────────────────────────────––––┘
```

This initial architecture passes through any non-QUIC traffic, and any traffic for which the FPGA does not have the keys. A future improvement may add a join-time option for a server to drop 1RTT traffic for which the FPGA does not have keys. The drawback of this would be an increased connection spin-up latency, for a benefit of further reducing bad traffic reaching the server.

The "Frame Swap" takes stream frames tagged by Wiredancer and replaces them with `RESET_STREAM`.

----
  
<br>

### 1. Encryption

Any solution for encryption will require the server to pass the session key for each connection that it wishes to have filtered. An ideal solution will do this with the key passed to the FPGA in an encrypted fashion, and in-band to the bidirectional network traffic that the FPGA is observing. This system doesn’t require the FPGA to understand handshake or initial packets. 

Once the server has finished the TLS handshake, it sends a `HANDSHAKE_DONE` packet that is 1RTT encrypted. The QUIC server will be modified such that it does not send 1RTT data until after the `HANDSHAKE_DONE`, and the `HANDSHAKE_DONE` is the only frame in the first 1RTT QUIC packet & UDP Datagram. The packet will be guaranteed to have enough space for them since the `HANDSHAKE_DONE` is small. The server will then encrypt the client traffic secret with a fixed FPGA pubkey, and append it, along with the server’s CID, to the end of the UDP datagram after the QUIC packet. The FPGA knows the first 1RTT encrypted packet for a specific dst.cid from a server IP has the client secret at the end of the datagram. The FPGA will extract the secret, and forward the UDP datagram without the keys appended.

```
+-------------------+
|    UDP Header     |
+-------------------+ <- Start of QUIC packet 
| QUIC Short Header | 
+-------------------+
|  HANDSHAKE_DONE   |
+-------------------+ <- End of QUIC packet. UDP ends here after FPGA strips secret
|   SECRET_STRUCT   |
+-------------------+
```

where `SECRET_STRUCT` looks like:

```
pub struct SECRET_STRUCT{
magic: String = "CLIENTKEY:", // Magic Key
cid: u64,                     // Server's CID
secret: vec<u8>               // Client Secret for this connection- 48 bytes.
}
```

At this point the FPGA derives the keys from the secret, and files the secret and keys in association with the Server’s IP and the CID included with the key. It now has the information it needs to decrypt client→server traffic, and can modify and re-encrypt the traffic as needed. The ephemeral session keys have been passed to the FPGA in an encrypted fashion such that other middleboxes cannot decrypt them.

In this scheme, all communication between the Server and FPGA is done in-band by modifying existing packets in the connection. If the server does not receive an ACK from the client for the packet containing the `HANDSHAKE_DONE`, then it knows the FPGA probably didn't get it.

> 💡 RFC9000 Section 13.3 says that the `HANDSHAKE_DONE` frame **must**  be retransmitted until it is acknowledged. This means that the QUIC protocol will ensure the FPGA receives the session client secret by appending them to the `HANDSHAKE_DONE`.

With this system, the client may send 1-RTT encrypted data before the FPGA has the keys. There are two approaches the FPGA can take with data it can’t decrypt yet: The FPGA can allow through 1-RTT data for which it does not have keys, or the FPGA can drop such traffic. Initial implementation will have the FPGA pass 1RTT traffic for which it does not have keys. Eventually perhaps DZ will provide a subscription-time option for a validator to choose whether such data is dropped or passed. If the FPGA drops pre-key traffic, the drop will cause the client to re-send any ack-eliciting frames since the packet is actually dropped.


#### **Required Validator Code Modifications:**

Instantiate the QUIC server with the key injection enabled.


#### **Required QUIC Implementation Modifications**:

QUIC implementation must ensure only `HANDSHAKE_DONE` is in the first 1RTT encrypted packet, and that packet is the only thing in its UDP Datagram. Then it must extract, encrypt, and inject the keys into the datagram after the QUIC packet.

----

<br>

### 2. Flow Control

Assuming that the cryptographic problem is solved, the FPGA needs a way to handle the QUIC connection once it determines that it wants to drop a stream frame due to edge filtering logic. Unlike in UDP, the FPGA cannot drop the packet or frame. First, the client will re-try sending until the packet is acknowledged. Second, QUIC’s built in flow control will eventually cause the connection to stall because the server will not keep advancing the `MAX_DATA` window since it will not have received the amount of data that the client has sent, if the packet is received but has been shorted by dropping the frame.

To solve this, the FPGA will substitute the `STREAM` frame it wishes to drop with a `RESET_STREAM`, providing a Final Length parameter to account for length of the stream that is being dropped. The initial approach is to add together the Offset & Length fields from the frame being discarded. If the frame being dropped contains FIN flag, then this is accurate for the final length of the stream. However if the frame does not have the FIN flag, then there’s a reasonable likelihood more data will be coming and the Final Length generated will be too small. There two possible outcomes from an incorrect Final Length:

1. If the Final Length is too short and another frame from the stream (or a FIN) is received that implies a longer length, then the server may close the connection with a `FINAL_SIZE_ERROR`. However according to RFC9000 section 4.5, the server is not required to generate a `FINAL_SIZE_ERROR`. A possible modification could introduce handling code into this “error” case that instead adds the delta the `MAX_DATA` calculus and move the window forward appropriately. To do this requires keeping around the old Final Length for a stream that is otherwise terminated (as would generating the error in the first place).

2. If the Final Length is too long, then the server will think it has already received more data than the client thinks it has sent so far. In this case there is a risk that the client will send data that puts it over the `MAX_DATA` limit. This would cause the server to issue a `CONNECTION_CLOSE` . This should be avoided. We do not want to accidentally send an oversized Final Size if the `MAX_DATA` parameter is set to anything less than 2^62.
    
----

> 💡 Agave uses `MAX_STREAMS` as the primary method of rate limiting incoming transactions per client, and uses the `MAX_DATA` as a backstop. If Agave instead set the `MAX_DATA` to a huge value, the FPGA could always use a 4k FINAL Length parameter since the `MAX_DATA` parameter would be effectively unlimited (~563 trillion 4k transactions), and the client will not overrun it. 
Firedancer imposes no limits and sets both `MAX_DATA` and `MAX_STREAMS` to 2^62, so would already work with this approach.

----

As a result of this discrepancy between the two validator software stacks, there are two proposed options for the `FINAL_SIZE`: If Agave is changed to match Firedancer with a 2^62 `MAX_DATA`, then the FPGA will always set `FINAL_LENGTH` to 4k. If Agave continues to use the `MAX_DATA` backstop, then the FPGA will make a best guess based on offset+len, and it is recommended (but not necessary) that Quinn is modified based on the recommendation in #1 above.

#### **Required Validator Code Modifications:**

Agave: In the `RESET_STREAM` the FPGA will introduce an application code to indicate that this reset was FPGA interference. Then the validator software would need to handle/ignore this “error”. For example the Agave code in `agave/streamer/src/nonblocking/quic.rs` : `handle_connection()` would need a small change handle with this “error” case explicitly or else it looks like the current logic would close the connection.


#### **Requested Validator Code Modification**

Agave: Instantiate QUIC server with `MAX_DATA` to 2^62 so that the FPGA can always use a 4096 `FINAL_LENGTH`


#### **QUIC Server Modifications:**

Agave: This approach would generally work without any changes to the underlying QUIC library, especially with a 2^62 `MAX_DATA`. However, there are two Quinn modifications that would reduce the odds of an unexpected `CONNECTION_CLOSE` if the `MAX_DATA` is still used as it is currently:

- if Agave continues to use `MAX_DATA` as a backstop, it will be more reliable if the changes described in (1) above are made to Quinn. 

- if Quinn receives a `RESET_STREAM` with a different `FINAL_LENGTH` then it has already determined, Quinn must not issue a `FNAL_SIZE_ERROR`. This is slightly different from the previous point, and addresses an edge case where the `RESET_STREAM` replaced a packet without the Fin but, but that packet has already been received by the server.

**4k Transactions & Fragmentation:** Transactions which are fragmented across multiple packets may not be dropped until the last packet depending on the criteria causing the drop. There isn’t a workaround for this other than storing and forwarding, which is not practicable for the amount of traffic a single edge filtering node might be handling. The FPGA must issue the `RESET_STREAM` as soon as it knows that a drop is desired.


----

<br>

### 3. Packet Formatting
Some items in this section would require client changes, since they enforce formatting requirements on packets sent from TPU clients. Below is an initial set of changes. More may come during implementation, and this RFC will be amended.

<br>

#### **Necessary Changes**

**Connection IDs**: The server's CID must be 8 bytes. The server's CID must not rotate throughout the life of a connection. For middlebox parsing, CID must be a fixed length, because short header packets assume the recipient knows the Destination CID length. The FPGA needs to store keys in association with a particular connection, so the CID should not rotate.

💡 *As long as the Server’s CID is a fixed length, the FPGA can filter inbound traffic. If we ever need to parse data sent towards the client, the client’s CID must be fixed too. Effort should be made to encourage clients to enforce these same rules*

<br>

**Packet Fragmentation**: Stream frames must be 1232 bytes, except for the last frame in a stream. If a frame is shorter than 1232 bytes and does not have the FIN flag, then the FPGA must replace it with a `RESET_STREAM` to prevent abuse of the connection.

> 💡 This ensures the FPGA has the most information possible as early as possible, that transactions are broken up into a predictable pattern, and that a malicious sender does not break a transaction into tiny pieces to get around filtering. Currently Agave allows smaller frames, but only four total fragments. Since there are already rules about fragmentation, this adjustment of those rules allows the Edge Filtration to be more useful and efficient. This is already usually met by a normal sender.

> 👉 There is an option to instead enforce some smaller (but reasonable) minimum size for the first frame in a stream. For example, requiring all the signatures, or signatures + header of a transaction to be in the first frame. However once some size constraint must be enforced, we might as well enforce something that will make both Validator and FPGA code paths more efficient (thus optimizing for processor time, rather than network bandwidth).

<br>

**Encryption**: There must be a single set of encryption standards for data and headers. This must be (matching Quinn defaults):

*Cipher Suite:* TLS_AES_256_GCM_SHA384

*Header Protection:* AES-ECB

*Packet Protection:* AES-GCM

The FPGA will have to keep up with key changes as they rotate using the Client Secret passed initially. If the FPGA can no longer decode 1RTT packets in a connection for which it previously had keys, it may issue a `CONNECTION_CLOSE`.

<br>

#### **Optional**

**Coalescing:** Short header packets must not be coalesced, even after a long-header packet. If the UDP datagram contains a QUIC short header packet, then that must be the first and only thing in the packet.

**Frame Ordering:** Stream Frames must be the first thing in a QUIC packet.

**Single Stream Frame per Packet**: There must be only one Stream frame in a QUIC packet.

*Taken together, these three would make it so the FPGA only needs to process the first frame in any short-header packet, and does not need to be able to parse any frame types other than stream frames to filter transactions.*

<br>

## Impact

### Solana Validator Software
Both Solana Validator software clients will require some changes:

- Ensure `HANDSHAKE_DONE` is alone in a QUIC Packet & UDP Datagram
- If subscribed to edge filtering, embed Client Session key in `HANDSHAKE_DONE`
- Handle `RESET_STREAM` application codes 
- When acting as Transaction Sender, update QUIC libraries to ensure correct fragmentation of stream data.
- When doing TLS handshakes, must negotiate for only the single accepted encryption scheme.

Expected Performance Impacts:

- Changes to fragmentation may result in a marginal increase in network bandwidth usage, but will likely result in improved processor performance on the Rx side.

<br>

### Solana Ecosystem
- Solana transaction senders must be updated to ensure the stream frames are fragmented properly.

    > 👉 Recommendation: Early FPGA implementations might not drop streams with incorrect frame fragmentation. This would allow more time for transaction senders to adjust behavior. The feasibility of this depends on several factors.

- Solana transaction senders should migrate to 8 byte CIDs to ensure future compatibility

- Solana transaction senders should adopt the "optional" recommendations from section 3. This will allow future performance improvements to filtering and validator behavior.

<br>

### FPGA Codebase

The existing Wiredancer FPGA code will require several changes to make this work:
- Likely needs to understand frames within QUIC packets, and tag, rather than drop bad frames
- Support for fragmentation
- Will need the entire "QUIC Parser" module from the Architecture section

<br>

## Security Considerations

There are several security considerations to take into account with this RFC

### Sharing Session Secrets
Session secrets will be passed to the FPGA encrypted by an FPGA pubkey so that any other snoopers of network traffic cannot intercept them. If the FPGA's private key is compromised, then it can be rotated, and the validator software updated to match the new key. Since session secrets are ephemeral, previously captured secrets have no future value. Any validator or sender with concerns that a particular session may have been compromised needs only to disconnect and reconnect to establish new secrets.

### FPGA Access to Transactions
Some in the Solana community may be concerned that the DoubleZero FPGA will have access to transaction data as it passes through. The Solana Core Dev community has agreed that since DoubleZero is a trusted contributor to the Solana ecosystem, this is acceptable. A developer of Validator software would have similar access to transaction flow. Additionally, until recently the transactions were not encrypted in the first place, and the change to QUIC for TPU was for the purpose of flow control, not encryption. Any validator who does not wish to allow the DoubleZero FPGA this access can choose not opt into edge filtering.

### Bogus Session Secrets
The FPGA trusts that session secrets coming from a server's IP are, in fact, from that server. If a malicious actor could spoof the server's IP, they could provide a bogus session secret for a connection, thus either disabling filtering for that connection or causing all traffic on that connection to be dropped. 

This is mitigated because traffic from the server originates inside the DoubleZero edge filtration VRF. As such DoubleZero will have other systems in place to ensure that the source IP won't be spoofed, see RFCxx for more on edge filtration routing.

<br>

## Backward Compatibility

Validators: There should be no backward compatibility related issues for Validators. A validator that is not capable of sharing keys will not have traffic filtered and will see no behavior change. 

RPCs/Senders: There may be backward compatibility issues for transaction senders. Once the fragmentation requirements are enforced, senders will see their transactions dropped if they do not comply with the stream fragmentation requirements. If the implementation details make this possible, we will phase this in over time to allow update time. 

FPGA: None are deployed yet.

## Open Questions

- How does the FPGA to handle packets that get re-transmitted? The FPGA could see a repeat of a stream fragment in two ways:
    - If a packet is lost after it passes through the FPGA
    - If stream frames arrive out of order, Firedancer will throw away the packet and not ack it until the previous frames in the stream arrive.

    💡 Handling this may require enforcing the fragmentation rules (max size frames until the last one).

- How is this going to integrate this with existing WD code? Separate frames from packets and then recombine, or adjust RTL to understand QUIC frames?


*Items that still need resolution.*
List outstanding issues, research tasks, or decisions deferred to later milestones. This section helps reviewers focus feedback and signals areas where contributions are welcomed.
