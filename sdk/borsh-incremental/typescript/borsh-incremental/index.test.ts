import { describe, test, expect } from "bun:test";
import { IncrementalReader } from "./index";

// --- Helpers ---

function u16le(v: number): Uint8Array {
  const buf = new ArrayBuffer(2);
  new DataView(buf).setUint16(0, v, true);
  return new Uint8Array(buf);
}

function u32le(v: number): Uint8Array {
  const buf = new ArrayBuffer(4);
  new DataView(buf).setUint32(0, v, true);
  return new Uint8Array(buf);
}

function u64le(v: bigint): Uint8Array {
  const buf = new ArrayBuffer(8);
  new DataView(buf).setBigUint64(0, v, true);
  return new Uint8Array(buf);
}

function f64le(v: number): Uint8Array {
  const buf = new ArrayBuffer(8);
  new DataView(buf).setFloat64(0, v, true);
  return new Uint8Array(buf);
}

function concat(...arrays: Uint8Array[]): Uint8Array {
  const total = arrays.reduce((s, a) => s + a.length, 0);
  const result = new Uint8Array(total);
  let offset = 0;
  for (const a of arrays) {
    result.set(a, offset);
    offset += a.length;
  }
  return result;
}

function strBorsh(s: string): Uint8Array {
  const encoded = new TextEncoder().encode(s);
  return concat(u32le(encoded.length), encoded);
}

const EMPTY = new Uint8Array(0);

// --- Tests ---

describe("IncrementalReader", () => {
  describe("offset and remaining getters", () => {
    test("initial state", () => {
      const r = new IncrementalReader(new Uint8Array([1, 2, 3]));
      expect(r.offset).toBe(0);
      expect(r.remaining).toBe(3);
    });

    test("after reads", () => {
      const r = new IncrementalReader(new Uint8Array([1, 2, 3]));
      r.readU8();
      expect(r.offset).toBe(1);
      expect(r.remaining).toBe(2);
    });

    test("empty buffer", () => {
      const r = new IncrementalReader(EMPTY);
      expect(r.offset).toBe(0);
      expect(r.remaining).toBe(0);
    });
  });

  describe("happy path for every read* method", () => {
    test("readU8", () => {
      const r = new IncrementalReader(new Uint8Array([0xff]));
      expect(r.readU8()).toBe(255);
      expect(r.offset).toBe(1);
    });

    test("readBool true and false", () => {
      const r = new IncrementalReader(new Uint8Array([1, 0]));
      expect(r.readBool()).toBe(true);
      expect(r.readBool()).toBe(false);
      expect(r.offset).toBe(2);
    });

    test("readU16", () => {
      const r = new IncrementalReader(u16le(0x1234));
      expect(r.readU16()).toBe(0x1234);
      expect(r.offset).toBe(2);
    });

    test("readU32", () => {
      const r = new IncrementalReader(u32le(0xdeadbeef));
      expect(r.readU32()).toBe(0xdeadbeef);
      expect(r.offset).toBe(4);
    });

    test("readU64", () => {
      const r = new IncrementalReader(u64le(123456789012345n));
      expect(r.readU64()).toBe(123456789012345n);
      expect(r.offset).toBe(8);
    });

    test("readU128", () => {
      const low = 0xdeadbeef12345678n;
      const high = 0x1n;
      const r = new IncrementalReader(concat(u64le(low), u64le(high)));
      expect(r.readU128()).toBe(low | (high << 64n));
      expect(r.offset).toBe(16);
    });

    test("readF64", () => {
      const r = new IncrementalReader(f64le(3.14));
      expect(r.readF64()).toBeCloseTo(3.14, 10);
      expect(r.offset).toBe(8);
    });

    test("readBytes", () => {
      const r = new IncrementalReader(new Uint8Array([10, 20, 30]));
      const bytes = r.readBytes(2);
      expect(bytes).toEqual(new Uint8Array([10, 20]));
      expect(r.offset).toBe(2);
    });

    test("readPubkeyRaw", () => {
      const key = new Uint8Array(32).fill(0xab);
      const r = new IncrementalReader(key);
      expect(r.readPubkeyRaw()).toEqual(key);
      expect(r.offset).toBe(32);
    });

    test("readIPv4", () => {
      const ip = new Uint8Array([192, 168, 1, 1]);
      const r = new IncrementalReader(ip);
      expect(r.readIPv4()).toEqual(ip);
      expect(r.offset).toBe(4);
    });

    test("readNetworkV4", () => {
      const net = new Uint8Array([10, 0, 0, 0, 24]);
      const r = new IncrementalReader(net);
      expect(r.readNetworkV4()).toEqual(net);
      expect(r.offset).toBe(5);
    });

    test("readString", () => {
      const r = new IncrementalReader(strBorsh("hello"));
      expect(r.readString()).toBe("hello");
      expect(r.offset).toBe(4 + 5);
    });

    test("readPubkeyRawVec", () => {
      const k1 = new Uint8Array(32).fill(1);
      const k2 = new Uint8Array(32).fill(2);
      const r = new IncrementalReader(concat(u32le(2), k1, k2));
      const result = r.readPubkeyRawVec();
      expect(result).toHaveLength(2);
      expect(result[0]).toEqual(k1);
      expect(result[1]).toEqual(k2);
    });

    test("readNetworkV4Vec", () => {
      const n1 = new Uint8Array([10, 0, 0, 0, 8]);
      const n2 = new Uint8Array([172, 16, 0, 0, 12]);
      const r = new IncrementalReader(concat(u32le(2), n1, n2));
      const result = r.readNetworkV4Vec();
      expect(result).toHaveLength(2);
      expect(result[0]).toEqual(n1);
      expect(result[1]).toEqual(n2);
    });
  });

  describe("throw on empty buffer for every read*", () => {
    test("readU8", () => expect(() => new IncrementalReader(EMPTY).readU8()).toThrow());
    test("readBool", () => expect(() => new IncrementalReader(EMPTY).readBool()).toThrow());
    test("readU16", () => expect(() => new IncrementalReader(EMPTY).readU16()).toThrow());
    test("readU32", () => expect(() => new IncrementalReader(EMPTY).readU32()).toThrow());
    test("readU64", () => expect(() => new IncrementalReader(EMPTY).readU64()).toThrow());
    test("readU128", () => expect(() => new IncrementalReader(EMPTY).readU128()).toThrow());
    test("readF64", () => expect(() => new IncrementalReader(EMPTY).readF64()).toThrow());
    test("readBytes", () => expect(() => new IncrementalReader(EMPTY).readBytes(1)).toThrow());
    test("readPubkeyRaw", () => expect(() => new IncrementalReader(EMPTY).readPubkeyRaw()).toThrow());
    test("readIPv4", () => expect(() => new IncrementalReader(EMPTY).readIPv4()).toThrow());
    test("readNetworkV4", () => expect(() => new IncrementalReader(EMPTY).readNetworkV4()).toThrow());
    test("readString", () => expect(() => new IncrementalReader(EMPTY).readString()).toThrow());
    test("readPubkeyRawVec", () => expect(() => new IncrementalReader(EMPTY).readPubkeyRawVec()).toThrow());
    test("readNetworkV4Vec", () => expect(() => new IncrementalReader(EMPTY).readNetworkV4Vec()).toThrow());
  });

  describe("partial data throw for multi-byte read*", () => {
    test("readU16 with 1 byte", () => {
      expect(() => new IncrementalReader(new Uint8Array([0x01])).readU16()).toThrow();
    });

    test("readU32 with 2 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array([0, 0])).readU32()).toThrow();
    });

    test("readU64 with 4 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(4)).readU64()).toThrow();
    });

    test("readU128 with 12 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(12)).readU128()).toThrow();
    });

    test("readF64 with 5 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(5)).readF64()).toThrow();
    });

    test("readPubkeyRaw with 20 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(20)).readPubkeyRaw()).toThrow();
    });

    test("readIPv4 with 3 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(3)).readIPv4()).toThrow();
    });

    test("readNetworkV4 with 4 bytes", () => {
      expect(() => new IncrementalReader(new Uint8Array(4)).readNetworkV4()).toThrow();
    });

    test("readBytes with fewer bytes than requested", () => {
      expect(() => new IncrementalReader(new Uint8Array(2)).readBytes(5)).toThrow();
    });
  });

  describe("tryRead* returns default on empty buffer", () => {
    test("tryReadU8", () => expect(new IncrementalReader(EMPTY).tryReadU8()).toBe(0));
    test("tryReadU8 custom default", () => expect(new IncrementalReader(EMPTY).tryReadU8(42)).toBe(42));
    test("tryReadBool", () => expect(new IncrementalReader(EMPTY).tryReadBool()).toBe(false));
    test("tryReadBool custom default", () => expect(new IncrementalReader(EMPTY).tryReadBool(true)).toBe(true));
    test("tryReadU16", () => expect(new IncrementalReader(EMPTY).tryReadU16()).toBe(0));
    test("tryReadU32", () => expect(new IncrementalReader(EMPTY).tryReadU32()).toBe(0));
    test("tryReadU64", () => expect(new IncrementalReader(EMPTY).tryReadU64()).toBe(0n));
    test("tryReadU128", () => expect(new IncrementalReader(EMPTY).tryReadU128()).toBe(0n));
    test("tryReadF64", () => expect(new IncrementalReader(EMPTY).tryReadF64()).toBe(0));
    test("tryReadPubkeyRaw", () => {
      expect(new IncrementalReader(EMPTY).tryReadPubkeyRaw()).toEqual(new Uint8Array(32));
    });
    test("tryReadIPv4", () => {
      expect(new IncrementalReader(EMPTY).tryReadIPv4()).toEqual(new Uint8Array(4));
    });
    test("tryReadNetworkV4", () => {
      expect(new IncrementalReader(EMPTY).tryReadNetworkV4()).toEqual(new Uint8Array(5));
    });
    test("tryReadString", () => expect(new IncrementalReader(EMPTY).tryReadString()).toBe(""));
    test("tryReadPubkeyRawVec", () => {
      expect(new IncrementalReader(EMPTY).tryReadPubkeyRawVec()).toEqual([]);
    });
    test("tryReadNetworkV4Vec", () => {
      expect(new IncrementalReader(EMPTY).tryReadNetworkV4Vec()).toEqual([]);
    });
  });

  describe("tryRead* returns actual value when data exists", () => {
    test("tryReadU8", () => expect(new IncrementalReader(new Uint8Array([7])).tryReadU8()).toBe(7));
    test("tryReadBool", () => expect(new IncrementalReader(new Uint8Array([1])).tryReadBool()).toBe(true));
    test("tryReadU16", () => expect(new IncrementalReader(u16le(500)).tryReadU16()).toBe(500));
    test("tryReadU32", () => expect(new IncrementalReader(u32le(100000)).tryReadU32()).toBe(100000));
    test("tryReadU64", () => expect(new IncrementalReader(u64le(999n)).tryReadU64()).toBe(999n));
    test("tryReadU128", () => {
      const r = new IncrementalReader(concat(u64le(42n), u64le(0n)));
      expect(r.tryReadU128()).toBe(42n);
    });
    test("tryReadF64", () => {
      expect(new IncrementalReader(f64le(2.718)).tryReadF64()).toBeCloseTo(2.718, 10);
    });
    test("tryReadPubkeyRaw", () => {
      const key = new Uint8Array(32).fill(0xcc);
      expect(new IncrementalReader(key).tryReadPubkeyRaw()).toEqual(key);
    });
    test("tryReadIPv4", () => {
      const ip = new Uint8Array([10, 0, 0, 1]);
      expect(new IncrementalReader(ip).tryReadIPv4()).toEqual(ip);
    });
    test("tryReadNetworkV4", () => {
      const net = new Uint8Array([192, 168, 0, 0, 16]);
      expect(new IncrementalReader(net).tryReadNetworkV4()).toEqual(net);
    });
    test("tryReadString", () => {
      expect(new IncrementalReader(strBorsh("world")).tryReadString()).toBe("world");
    });
    test("tryReadPubkeyRawVec", () => {
      const k = new Uint8Array(32).fill(0xaa);
      const r = new IncrementalReader(concat(u32le(1), k));
      expect(r.tryReadPubkeyRawVec()).toEqual([k]);
    });
    test("tryReadNetworkV4Vec", () => {
      const n = new Uint8Array([1, 2, 3, 4, 5]);
      const r = new IncrementalReader(concat(u32le(1), n));
      expect(r.tryReadNetworkV4Vec()).toEqual([n]);
    });
  });

  describe("tryRead* with partial data returns default", () => {
    test("tryReadU16 with 1 byte", () => {
      expect(new IncrementalReader(new Uint8Array([0x01])).tryReadU16()).toBe(0);
    });

    test("tryReadU32 with 1 byte", () => {
      expect(new IncrementalReader(new Uint8Array([0x01])).tryReadU32()).toBe(0);
    });

    test("tryReadU64 with 3 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(3)).tryReadU64()).toBe(0n);
    });

    test("tryReadU128 with 10 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(10)).tryReadU128()).toBe(0n);
    });

    test("tryReadF64 with 6 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(6)).tryReadF64()).toBe(0);
    });

    test("tryReadPubkeyRaw with 16 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(16)).tryReadPubkeyRaw()).toEqual(new Uint8Array(32));
    });

    test("tryReadIPv4 with 2 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(2)).tryReadIPv4()).toEqual(new Uint8Array(4));
    });

    test("tryReadNetworkV4 with 3 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(3)).tryReadNetworkV4()).toEqual(new Uint8Array(5));
    });

    test("tryReadString with 2 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(2)).tryReadString()).toBe("");
    });

    test("tryReadPubkeyRawVec with 2 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(2)).tryReadPubkeyRawVec()).toEqual([]);
    });

    test("tryReadNetworkV4Vec with 2 bytes", () => {
      expect(new IncrementalReader(new Uint8Array(2)).tryReadNetworkV4Vec()).toEqual([]);
    });
  });

  describe("sequential reads verify offset tracking", () => {
    test("read u8 then u32 then u16", () => {
      const data = concat(new Uint8Array([42]), u32le(1000), u16le(500));
      const r = new IncrementalReader(data);
      expect(r.readU8()).toBe(42);
      expect(r.offset).toBe(1);
      expect(r.readU32()).toBe(1000);
      expect(r.offset).toBe(5);
      expect(r.readU16()).toBe(500);
      expect(r.offset).toBe(7);
      expect(r.remaining).toBe(0);
    });

    test("read string then u64 then bool", () => {
      const data = concat(strBorsh("abc"), u64le(99n), new Uint8Array([1]));
      const r = new IncrementalReader(data);
      expect(r.readString()).toBe("abc");
      expect(r.offset).toBe(7);
      expect(r.readU64()).toBe(99n);
      expect(r.offset).toBe(15);
      expect(r.readBool()).toBe(true);
      expect(r.offset).toBe(16);
      expect(r.remaining).toBe(0);
    });
  });

  describe("trailing fields scenario", () => {
    test("strict fields followed by optional tryRead fields", () => {
      // Simulate a struct with required u32 + optional u8 trailing field
      const data = concat(u32le(42));
      const r = new IncrementalReader(data);
      expect(r.readU32()).toBe(42);
      // trailing field not present, tryRead returns default
      expect(r.tryReadU8(255)).toBe(255);
      expect(r.offset).toBe(4);
    });

    test("strict fields followed by present trailing field", () => {
      const data = concat(u32le(42), new Uint8Array([7]));
      const r = new IncrementalReader(data);
      expect(r.readU32()).toBe(42);
      expect(r.tryReadU8(255)).toBe(7);
      expect(r.offset).toBe(5);
    });
  });

  describe("Vec methods", () => {
    test("empty pubkey vec", () => {
      const r = new IncrementalReader(u32le(0));
      expect(r.readPubkeyRawVec()).toEqual([]);
      expect(r.offset).toBe(4);
    });

    test("single element pubkey vec", () => {
      const k = new Uint8Array(32).fill(0x11);
      const r = new IncrementalReader(concat(u32le(1), k));
      const result = r.readPubkeyRawVec();
      expect(result).toHaveLength(1);
      expect(result[0]).toEqual(k);
    });

    test("multiple element pubkey vec", () => {
      const k1 = new Uint8Array(32).fill(0x01);
      const k2 = new Uint8Array(32).fill(0x02);
      const k3 = new Uint8Array(32).fill(0x03);
      const r = new IncrementalReader(concat(u32le(3), k1, k2, k3));
      const result = r.readPubkeyRawVec();
      expect(result).toHaveLength(3);
      expect(result[2]).toEqual(k3);
    });

    test("truncated pubkey vec throws", () => {
      // says 2 elements but only has 1
      const k = new Uint8Array(32).fill(0x01);
      const r = new IncrementalReader(concat(u32le(2), k));
      expect(() => r.readPubkeyRawVec()).toThrow();
    });

    test("empty networkV4 vec", () => {
      const r = new IncrementalReader(u32le(0));
      expect(r.readNetworkV4Vec()).toEqual([]);
    });

    test("multiple element networkV4 vec", () => {
      const n1 = new Uint8Array([10, 0, 0, 0, 8]);
      const n2 = new Uint8Array([172, 16, 0, 0, 12]);
      const r = new IncrementalReader(concat(u32le(2), n1, n2));
      const result = r.readNetworkV4Vec();
      expect(result).toHaveLength(2);
    });

    test("truncated networkV4 vec throws", () => {
      const n = new Uint8Array([10, 0, 0, 0, 8]);
      const r = new IncrementalReader(concat(u32le(3), n));
      expect(() => r.readNetworkV4Vec()).toThrow();
    });
  });

  describe("String", () => {
    test("empty string", () => {
      const r = new IncrementalReader(u32le(0));
      expect(r.readString()).toBe("");
      expect(r.offset).toBe(4);
    });

    test("normal string", () => {
      const r = new IncrementalReader(strBorsh("hello world"));
      expect(r.readString()).toBe("hello world");
    });

    test("truncated string throws (length says 10 but only 5 bytes)", () => {
      const partial = concat(u32le(10), new Uint8Array([65, 66, 67, 68, 69]));
      const r = new IncrementalReader(partial);
      expect(() => r.readString()).toThrow();
    });
  });

  describe("U128 little-endian byte order", () => {
    test("low word only", () => {
      const r = new IncrementalReader(concat(u64le(0xffffffffffffffffn), u64le(0n)));
      expect(r.readU128()).toBe(0xffffffffffffffffn);
    });

    test("high word only", () => {
      const r = new IncrementalReader(concat(u64le(0n), u64le(1n)));
      expect(r.readU128()).toBe(1n << 64n);
    });

    test("both words", () => {
      const low = 0x123456789abcdef0n;
      const high = 0xfedcba9876543210n;
      const r = new IncrementalReader(concat(u64le(low), u64le(high)));
      expect(r.readU128()).toBe(low | (high << 64n));
    });
  });

  describe("U64 bigint values", () => {
    test("zero", () => {
      expect(new IncrementalReader(u64le(0n)).readU64()).toBe(0n);
    });

    test("max u64", () => {
      const max = 0xffffffffffffffffn;
      expect(new IncrementalReader(u64le(max)).readU64()).toBe(max);
    });

    test("large value", () => {
      const v = 9999999999999999999n;
      expect(new IncrementalReader(u64le(v)).readU64()).toBe(v);
    });
  });

  describe("F64 known float", () => {
    test("pi", () => {
      expect(new IncrementalReader(f64le(Math.PI)).readF64()).toBe(Math.PI);
    });

    test("negative", () => {
      expect(new IncrementalReader(f64le(-1.5)).readF64()).toBe(-1.5);
    });

    test("zero", () => {
      expect(new IncrementalReader(f64le(0)).readF64()).toBe(0);
    });
  });

  describe("readBytes", () => {
    test("happy path", () => {
      const data = new Uint8Array([1, 2, 3, 4, 5]);
      const r = new IncrementalReader(data);
      expect(r.readBytes(3)).toEqual(new Uint8Array([1, 2, 3]));
      expect(r.readBytes(2)).toEqual(new Uint8Array([4, 5]));
      expect(r.remaining).toBe(0);
    });

    test("zero bytes", () => {
      const r = new IncrementalReader(new Uint8Array([1]));
      expect(r.readBytes(0)).toEqual(new Uint8Array(0));
      expect(r.offset).toBe(0);
    });

    test("error when not enough bytes", () => {
      const r = new IncrementalReader(new Uint8Array([1, 2]));
      expect(() => r.readBytes(10)).toThrow();
    });
  });
});
