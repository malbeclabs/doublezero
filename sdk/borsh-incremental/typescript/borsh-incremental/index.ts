/**
 * Borsh incremental deserialization reader.
 *
 * Provides cursor-based reading of Borsh-serialized binary data with
 * backward-compatible trailing field support via tryRead* methods.
 */

export class IncrementalReader {
  private data: DataView;
  private raw: Uint8Array;
  private _offset: number;

  constructor(data: Uint8Array) {
    this.raw = data;
    this.data = new DataView(data.buffer, data.byteOffset, data.byteLength);
    this._offset = 0;
  }

  get offset(): number {
    return this._offset;
  }

  get remaining(): number {
    return this.raw.byteLength - this._offset;
  }

  // --- Strict read methods (throw on insufficient data) ---

  readU8(): number {
    if (this._offset + 1 > this.raw.byteLength) {
      throw new Error(`borsh: not enough data for u8 at offset ${this._offset}`);
    }
    const v = this.data.getUint8(this._offset);
    this._offset += 1;
    return v;
  }

  readBool(): boolean {
    return this.readU8() !== 0;
  }

  readU16(): number {
    if (this._offset + 2 > this.raw.byteLength) {
      throw new Error(`borsh: not enough data for u16 at offset ${this._offset}`);
    }
    const v = this.data.getUint16(this._offset, true);
    this._offset += 2;
    return v;
  }

  readU32(): number {
    if (this._offset + 4 > this.raw.byteLength) {
      throw new Error(`borsh: not enough data for u32 at offset ${this._offset}`);
    }
    const v = this.data.getUint32(this._offset, true);
    this._offset += 4;
    return v;
  }

  readU64(): bigint {
    if (this._offset + 8 > this.raw.byteLength) {
      throw new Error(`borsh: not enough data for u64 at offset ${this._offset}`);
    }
    const v = this.data.getBigUint64(this._offset, true);
    this._offset += 8;
    return v;
  }

  readU128(): bigint {
    const low = this.readU64();
    const high = this.readU64();
    return low | (high << 64n);
  }

  readF64(): number {
    if (this._offset + 8 > this.raw.byteLength) {
      throw new Error(`borsh: not enough data for f64 at offset ${this._offset}`);
    }
    const v = this.data.getFloat64(this._offset, true);
    this._offset += 8;
    return v;
  }

  readBytes(n: number): Uint8Array {
    if (this._offset + n > this.raw.byteLength) {
      throw new Error(
        `borsh: not enough data for ${n} bytes at offset ${this._offset}`,
      );
    }
    const v = this.raw.slice(this._offset, this._offset + n);
    this._offset += n;
    return v;
  }

  readPubkeyRaw(): Uint8Array {
    return this.readBytes(32);
  }

  readIPv4(): Uint8Array {
    return this.readBytes(4);
  }

  readNetworkV4(): Uint8Array {
    return this.readBytes(5);
  }

  readString(): string {
    const len = this.readU32();
    if (len === 0) return "";
    if (this._offset + len > this.raw.byteLength) {
      throw new Error(
        `borsh: not enough data for string of length ${len} at offset ${this._offset}`,
      );
    }
    const bytes = this.raw.slice(this._offset, this._offset + len);
    this._offset += len;
    return new TextDecoder().decode(bytes);
  }

  readPubkeyRawVec(): Uint8Array[] {
    const len = this.readU32();
    const result: Uint8Array[] = [];
    for (let i = 0; i < len; i++) result.push(this.readPubkeyRaw());
    return result;
  }

  readNetworkV4Vec(): Uint8Array[] {
    const len = this.readU32();
    const result: Uint8Array[] = [];
    for (let i = 0; i < len; i++) result.push(this.readNetworkV4());
    return result;
  }

  // --- Try variants (return default when no bytes available) ---

  tryReadU8(def: number = 0): number {
    if (this.remaining < 1) return def;
    return this.readU8();
  }

  tryReadBool(def: boolean = false): boolean {
    if (this.remaining < 1) return def;
    return this.readBool();
  }

  tryReadU16(def: number = 0): number {
    if (this.remaining < 2) return def;
    return this.readU16();
  }

  tryReadU32(def: number = 0): number {
    if (this.remaining < 4) return def;
    return this.readU32();
  }

  tryReadU64(def: bigint = 0n): bigint {
    if (this.remaining < 8) return def;
    return this.readU64();
  }

  tryReadU128(def: bigint = 0n): bigint {
    if (this.remaining < 16) return def;
    return this.readU128();
  }

  tryReadF64(def: number = 0): number {
    if (this.remaining < 8) return def;
    return this.readF64();
  }

  tryReadPubkeyRaw(def: Uint8Array = new Uint8Array(32)): Uint8Array {
    if (this.remaining < 32) return def;
    return this.readPubkeyRaw();
  }

  tryReadIPv4(def: Uint8Array = new Uint8Array(4)): Uint8Array {
    if (this.remaining < 4) return def;
    return this.readIPv4();
  }

  tryReadNetworkV4(def: Uint8Array = new Uint8Array(5)): Uint8Array {
    if (this.remaining < 5) return def;
    return this.readNetworkV4();
  }

  tryReadString(def: string = ""): string {
    if (this.remaining < 4) return def;
    return this.readString();
  }

  tryReadPubkeyRawVec(def: Uint8Array[] = []): Uint8Array[] {
    if (this.remaining < 4) return def;
    return this.readPubkeyRawVec();
  }

  tryReadNetworkV4Vec(def: Uint8Array[] = []): Uint8Array[] {
    if (this.remaining < 4) return def;
    return this.readNetworkV4Vec();
  }
}

/**
 * Wrapper around IncrementalReader that uses tryRead for all operations.
 *
 * All read methods return zero/empty defaults on insufficient data, matching
 * Go's ByteReader behavior. This makes deserialization resilient to schema
 * changes where new fields are added to the end of structs.
 */
export class DefensiveReader {
  private r: IncrementalReader;

  constructor(data: Uint8Array) {
    this.r = new IncrementalReader(data);
  }

  get offset(): number {
    return this.r.offset;
  }

  get remaining(): number {
    return this.r.remaining;
  }

  readU8(): number {
    return this.r.tryReadU8(0);
  }

  readBool(): boolean {
    return this.r.tryReadBool(false);
  }

  readU16(): number {
    return this.r.tryReadU16(0);
  }

  readU32(): number {
    return this.r.tryReadU32(0);
  }

  readU64(): bigint {
    return this.r.tryReadU64(0n);
  }

  readU128(): bigint {
    return this.r.tryReadU128(0n);
  }

  readF64(): number {
    return this.r.tryReadF64(0);
  }

  readPubkeyRaw(): Uint8Array {
    return this.r.tryReadPubkeyRaw(new Uint8Array(32));
  }

  readIPv4(): Uint8Array {
    return this.r.tryReadIPv4(new Uint8Array(4));
  }

  readNetworkV4(): Uint8Array {
    return this.r.tryReadNetworkV4(new Uint8Array(5));
  }

  readString(): string {
    return this.r.tryReadString("");
  }

  readPubkeyRawVec(): Uint8Array[] {
    return this.r.tryReadPubkeyRawVec([]);
  }

  readNetworkV4Vec(): Uint8Array[] {
    return this.r.tryReadNetworkV4Vec([]);
  }

  readBytes(n: number): Uint8Array {
    if (this.r.remaining < n) {
      return new Uint8Array(n);
    }
    return this.r.readBytes(n);
  }
}
