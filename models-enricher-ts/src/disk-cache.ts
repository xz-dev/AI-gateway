// Persistent store for validated @cpa-upstream responses: one file per request identity under
// <cacheDir>/v2/, FIFO-evicted within a payload budget that survives restart.
import { mkdir, open, readdir, readFile, rename, unlink } from "node:fs/promises";
import { join } from "node:path";
import type { Logger } from "./log.ts";
import { errorMessage } from "./log.ts";

export const DISK_PAYLOAD_BUDGET = 100_000_000;

// Header: magic(4) | admission seq u64 | last success ms f64 | payload length u64.
const MAGIC = Buffer.from("MEC2");
const HEADER = 28;
const KEY = /^[0-9a-f]{64}$/;

type Meta = { seq: number; stored: number; size: number };

export interface DiskEntry {
  body: string;
  /** Last successful refresh, epoch milliseconds. */
  stored: number;
}

export class DiskCache {
  // Insertion order is admission (FIFO) order; replacing a key keeps its position.
  readonly #index = new Map<string, Meta>();
  #bytes = 0;
  #nextSeq = 1;
  #writes: Promise<void> = Promise.resolve();

  readonly dir: string;
  readonly budget: number;
  readonly log: Logger;

  private constructor(dir: string, budget: number, log: Logger) {
    this.dir = dir;
    this.budget = budget;
    this.log = log;
  }

  static async open(cacheDir: string, log: Logger, budget = DISK_PAYLOAD_BUDGET): Promise<DiskCache> {
    const dir = join(cacheDir, "v2");
    await mkdir(dir, { recursive: true, mode: 0o700 });
    const cache = new DiskCache(dir, budget, log);
    await cache.#scan();
    return cache;
  }

  get bytes(): number {
    return this.#bytes;
  }

  /** Retained keys in FIFO admission order. */
  keys(): string[] {
    return [...this.#index.keys()];
  }

  async get(key: string): Promise<DiskEntry | undefined> {
    const meta = this.#index.get(key);
    if (!meta) return undefined;
    try {
      const raw = await readFile(join(this.dir, key));
      const header = parseHeader(raw);
      if (!header || header.seq !== meta.seq || raw.length !== HEADER + header.size) throw new Error("corrupt cache entry");
      return { body: raw.subarray(HEADER).toString("utf8"), stored: header.stored };
    } catch (error) {
      this.log.warn("disk cache read failed", { key, err: errorMessage(error) });
      return undefined;
    }
  }

  /** Admits or replaces one response. A body larger than the whole budget is rejected without evicting anything. */
  put(key: string, body: string, stored: number): Promise<void> {
    const run = this.#writes.then(() => this.#put(key, body, stored));
    this.#writes = run.catch(() => undefined);
    return run;
  }

  async #put(key: string, body: string, stored: number): Promise<void> {
    if (!KEY.test(key)) throw new Error("invalid cache key");
    const payload = Buffer.from(body, "utf8");
    if (payload.length > this.budget) throw new Error("response exceeds cache payload budget");
    const old = this.#index.get(key);
    const seq = old?.seq ?? this.#nextSeq++;
    const tmp = join(this.dir, `${key}.${process.pid}.tmp`);
    const file = await open(tmp, "w", 0o600);
    try {
      await file.write(header(seq, stored, payload.length));
      await file.write(payload);
      await file.sync();
    } finally {
      await file.close();
    }
    let total = this.#bytes - (old?.size ?? 0);
    for (const [victim, meta] of this.#index) {
      if (total + payload.length <= this.budget) break;
      if (victim === key) continue;
      await this.#remove(victim);
      total -= meta.size;
    }
    try {
      await rename(tmp, join(this.dir, key));
    } catch (error) {
      await unlink(tmp).catch(() => undefined);
      throw error;
    }
    this.#bytes = total + payload.length;
    this.#index.set(key, { seq, stored, size: payload.length });
  }

  async #remove(key: string): Promise<void> {
    const meta = this.#index.get(key);
    if (!meta) return;
    this.#index.delete(key);
    this.#bytes -= meta.size;
    await unlink(join(this.dir, key)).catch(() => undefined);
  }

  async #scan(): Promise<void> {
    const found: Array<[string, Meta]> = [];
    for (const name of await readdir(this.dir)) {
      const path = join(this.dir, name);
      if (name.endsWith(".tmp")) {
        await unlink(path).catch(() => undefined);
        continue;
      }
      if (!KEY.test(name)) continue;
      const meta = await readMeta(path);
      if (meta) found.push([name, meta]);
      else await unlink(path).catch(() => undefined);
    }
    found.sort((a, b) => a[1].seq - b[1].seq);
    for (const [key, meta] of found) {
      this.#index.set(key, meta);
      this.#bytes += meta.size;
      this.#nextSeq = Math.max(this.#nextSeq, meta.seq + 1);
    }
    // A lowered budget or an interrupted write may leave the store over budget.
    for (const key of this.#index.keys()) {
      if (this.#bytes <= this.budget) break;
      await this.#remove(key);
    }
  }
}

function header(seq: number, stored: number, size: number): Buffer {
  const out = Buffer.alloc(HEADER);
  MAGIC.copy(out, 0);
  out.writeBigUInt64BE(BigInt(seq), 4);
  out.writeDoubleBE(stored, 12);
  out.writeBigUInt64BE(BigInt(size), 20);
  return out;
}

function parseHeader(raw: Buffer): Meta | undefined {
  if (raw.length < HEADER || !raw.subarray(0, 4).equals(MAGIC)) return undefined;
  return { seq: Number(raw.readBigUInt64BE(4)), stored: raw.readDoubleBE(12), size: Number(raw.readBigUInt64BE(20)) };
}

async function readMeta(path: string): Promise<Meta | undefined> {
  const file = await open(path, "r");
  try {
    const buf = Buffer.alloc(HEADER);
    const { bytesRead } = await file.read(buf, 0, HEADER, 0);
    const meta = bytesRead === HEADER ? parseHeader(buf) : undefined;
    if (!meta || (await file.stat()).size !== HEADER + meta.size) return undefined;
    return meta;
  } finally {
    await file.close();
  }
}
