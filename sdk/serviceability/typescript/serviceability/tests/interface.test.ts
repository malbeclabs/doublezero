/**
 * Hand-built byte-vector tests for the size-prefixed Interface reader.
 *
 * Each Interface element on the wire is (u16 size, u8 version, body), where
 * size includes the 3-byte prefix. The Device account stores this vec
 * immediately after max_multicast_publishers.
 */

import { describe, expect, test } from "bun:test";
import {
  ACCOUNT_TYPE_DEVICE,
  deserializeDevice,
} from "../state.js";

// CURRENT_INTERFACE_VERSION isn't exported; we hard-code the same value here
// (intentional — the wire format version is a stable cross-language constant).
const CURRENT_INTERFACE_VERSION = 4;

function u16(v: number): Uint8Array {
  const b = new Uint8Array(2);
  new DataView(b.buffer).setUint16(0, v, true);
  return b;
}

function u32(v: number): Uint8Array {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, v, true);
  return b;
}

function u64(v: bigint): Uint8Array {
  const b = new Uint8Array(8);
  new DataView(b.buffer).setBigUint64(0, v, true);
  return b;
}

function string(s: string): Uint8Array {
  const enc = new TextEncoder().encode(s);
  return concat(u32(enc.length), enc);
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

function newInterfaceBody(name: string): Uint8Array {
  return concat(
    new Uint8Array([0]), // status
    string(name),
    new Uint8Array([0]), // interface_type
    new Uint8Array([0]), // interface_cyoa
    new Uint8Array([0]), // interface_dia
    new Uint8Array([0]), // loopback_type
    u64(0n), // bandwidth
    u64(0n), // cir
    u16(0), // mtu
    new Uint8Array([0]), // routing_mode
    u16(0), // vlan_id
    new Uint8Array(5), // ip_net (NetworkV4: 4 bytes IP + 1 byte prefix)
    u16(0), // node_segment_idx
    new Uint8Array([0]), // user_tunnel_endpoint
    u32(0), // flex_algo_node_segments len = 0
  );
}

function newInterfaceSized(
  name: string,
  version: number = CURRENT_INTERFACE_VERSION,
  bodyOverride?: Uint8Array,
): Uint8Array {
  const body = bodyOverride ?? newInterfaceBody(name);
  const size = 3 + body.length;
  return concat(u16(size), new Uint8Array([version]), body);
}

// V1-disc legacy interface: chosen over V2 because the Python V2 reader
// consumes a trailing flex_algo_node_segments u32 that this TS V2 reader does
// not — keeping the cross-language shape simple.
function legacyInterfaceV1(name: string): Uint8Array {
  return concat(
    new Uint8Array([0]), // enum disc V1
    new Uint8Array([0]), // status
    string(name),
    new Uint8Array([0]), // interface_type
    new Uint8Array([0]), // loopback_type
    u16(0), // vlan_id
    new Uint8Array(5), // ip_net
    u16(0), // node_segment_idx
    new Uint8Array([0]), // user_tunnel_endpoint
  );
}

function buildDevice(
  numLegacy: number,
  names: string[],
  trailing: Uint8Array | null,
): Uint8Array {
  const parts: Uint8Array[] = [];
  parts.push(new Uint8Array([ACCOUNT_TYPE_DEVICE])); // account_type
  parts.push(new Uint8Array(32)); // owner
  parts.push(concat(u64(1n), u64(0n))); // index (u128 low+high LE)
  parts.push(new Uint8Array([0xff])); // bump_seed
  parts.push(new Uint8Array(32)); // location_pk
  parts.push(new Uint8Array(32)); // exchange_pk
  parts.push(new Uint8Array([0])); // device_type
  parts.push(new Uint8Array([1, 2, 3, 4])); // public_ip
  parts.push(new Uint8Array([1])); // status
  parts.push(string("dev-test")); // code
  parts.push(u32(0)); // dz_prefixes (empty)
  parts.push(new Uint8Array(32)); // metrics_publisher_pk
  parts.push(new Uint8Array(32)); // contributor_pk
  parts.push(string("default")); // mgmt_vrf
  parts.push(u32(numLegacy));
  for (const n of names) parts.push(legacyInterfaceV1(n));
  parts.push(u32(0)); // reference_count
  parts.push(u16(0)); // users_count
  parts.push(u16(0)); // max_users
  parts.push(new Uint8Array([0])); // device_health
  parts.push(new Uint8Array([0])); // device_desired_status
  parts.push(u16(0)); // unicast_users_count
  parts.push(u16(0)); // multicast_subscribers_count
  parts.push(u16(0)); // max_unicast_users
  parts.push(u16(0)); // max_multicast_subscribers
  parts.push(u16(0)); // reserved_seats
  parts.push(u16(0)); // multicast_publishers_count
  parts.push(u16(0)); // max_multicast_publishers
  if (trailing !== null) parts.push(trailing);
  return concat(...parts);
}

describe("size-prefixed Interface", () => {
  test("populated trailing vec", () => {
    // Cross-language framing assertion: empty-name body length is
    // 1+4+1+1+1+1+8+8+2+1+2+5+2+1+4 = 42, so size = 3 + 42 = 45.
    expect(3 + newInterfaceBody("").length).toBe(45);

    const trailing = concat(
      u32(2),
      newInterfaceSized("Eth1"),
      newInterfaceSized("Lo0"),
    );
    const raw = buildDevice(2, ["Eth1", "Lo0"], trailing);

    const dev = deserializeDevice(raw);
    expect(dev.deprecatedInterfaces.length).toBe(2);
    expect(dev.interfaces.length).toBe(2);
    expect(dev.interfaces[0]!.name).toBe("Eth1");
    expect(dev.interfaces[1]!.name).toBe("Lo0");
    expect(dev.interfaces[0]!.version).toBe(CURRENT_INTERFACE_VERSION);
    for (const ni of dev.interfaces) {
      const expected = 3 + newInterfaceBody(ni.name).length;
      expect(ni.size).toBe(expected);
    }
  });

  test("legacy account rebuilds interfaces", () => {
    const raw = buildDevice(2, ["Eth1", "Lo0"], null);

    const dev = deserializeDevice(raw);
    expect(dev.deprecatedInterfaces.length).toBe(2);
    expect(dev.interfaces.length).toBe(2);
    expect(dev.interfaces[0]!.name).toBe("Eth1");
    expect(dev.interfaces[1]!.name).toBe("Lo0");
    // Rebuilt entries are stamped with the current schema version and zero size.
    for (const ni of dev.interfaces) {
      expect(ni.version).toBe(CURRENT_INTERFACE_VERSION);
      expect(ni.size).toBe(0);
    }
  });

  test("trailing length mismatch throws", () => {
    const trailing = concat(u32(1), newInterfaceSized("Eth1"));
    const raw = buildDevice(2, ["Eth1", "Lo0"], trailing);

    expect(() => deserializeDevice(raw)).toThrow(
      /interfaces length 1 != deprecatedInterfaces length 2/,
    );
  });

  test("future version skips trailing bytes", () => {
    // Forge a version=5 element with 8 trailing junk bytes appended past the
    // known body. The reader must advance past start+size.
    const body = concat(
      newInterfaceBody("Future1"),
      new Uint8Array([0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe]),
    );
    const sized = newInterfaceSized("Future1", 5, body);
    const trailing = concat(u32(1), sized);
    const raw = buildDevice(1, ["Future1"], trailing);

    const dev = deserializeDevice(raw);
    expect(dev.interfaces.length).toBe(1);
    expect(dev.interfaces[0]!.version).toBe(5);
    expect(dev.interfaces[0]!.size).toBe(3 + body.length);
    expect(dev.interfaces[0]!.name).toBe("Future1");
  });
});
