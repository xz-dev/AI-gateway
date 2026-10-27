// Read-through cache for validated upstream responses.
// - authority: process memory only (32 MiB, oldest-inserted first); stale reuse only on 502/503/504,
//   deadlines and network errors; denials and invalid bodies drop the entry so it cannot resurface.
// - metadata (@cpa-upstream): persisted on disk; any refresh failure reuses the last success.
// Concurrent loads of one identity share a single fetch.
import { createHash } from "node:crypto";
import type { DiskCache } from "./disk-cache.ts";
import { HttpPool, InvalidResponseError, ReadStatusError, isCancellation, isDeadline, isTransient, type HttpRequest } from "./http.ts";
import type { Logger } from "./log.ts";
import { errorMessage } from "./log.ts";

export type CachePolicy = "none" | "authority" | "metadata";

export const READ_CACHE_TTL_MS = 5 * 60_000;
export const AUTHORITY_CACHE_BUDGET = 32 << 20;

export interface ReadOptions {
  signal?: AbortSignal;
  /** Force refresh: skip freshness reuse for this attempt. */
  bypass?: boolean;
  /** Called once per load that answered with a retained (stale) response. */
  onStale?: () => void;
}

type Cached = { body: string; stored: number };

export class MemoryCache {
  readonly #entries = new Map<string, Cached & { size: number }>();
  #bytes = 0;

  readonly budget: number;

  constructor(budget = AUTHORITY_CACHE_BUDGET) {
    this.budget = budget;
  }

  get bytes(): number {
    return this.#bytes;
  }

  keys(): string[] {
    return [...this.#entries.keys()];
  }

  get(key: string): Cached | undefined {
    return this.#entries.get(key);
  }

  /** Replacement keeps the key's position; other entries are evicted oldest-inserted first. */
  put(key: string, body: string, stored: number): void {
    const size = Buffer.byteLength(body);
    if (size > this.budget) throw new Error("response exceeds authority cache budget");
    const old = this.#entries.get(key);
    this.#bytes -= old?.size ?? 0;
    for (const [victim, entry] of this.#entries) {
      if (this.#bytes + size <= this.budget) break;
      if (victim === key) continue;
      this.#entries.delete(victim);
      this.#bytes -= entry.size;
    }
    this.#entries.set(key, { body, stored, size });
    this.#bytes += size;
  }

  remove(key: string): void {
    const entry = this.#entries.get(key);
    if (!entry) return;
    this.#entries.delete(key);
    this.#bytes -= entry.size;
  }
}

export class ReadCache {
  readonly memory: MemoryCache;
  readonly #flights = new Map<string, Promise<string>>();

  readonly disk: DiskCache | undefined;
  readonly log: Logger;
  /** Epoch milliseconds; replaceable in tests. */
  clock: () => number;

  constructor(disk: DiskCache | undefined, log: Logger, clock: () => number = Date.now, memoryBudget = AUTHORITY_CACHE_BUDGET) {
    this.disk = disk;
    this.log = log;
    this.clock = clock;
    this.memory = new MemoryCache(memoryBudget);
  }

  async load(key: string, policy: "authority" | "metadata", fetch: () => Promise<string>, options: ReadOptions = {}): Promise<string> {
    const { signal, bypass = false } = options;
    if (signal?.aborted) {
      // An exhausted earlier stage still uses retained metadata; a real cancellation does not.
      if (policy === "metadata" && isDeadline(signal.reason)) {
        const old = await this.#lookup(policy, key);
        if (old) return this.#stale(key, old, signal.reason, options);
      }
      throw signal.reason;
    }
    const flightKey = bypass ? `${key}:force` : key;
    let flight = this.#flights.get(flightKey);
    if (!flight) {
      // Freshness is checked inside the claimed flight, so a just-stored result is never fetched twice.
      flight = this.#run(key, policy, fetch, options).finally(() => this.#flights.delete(flightKey));
      this.#flights.set(flightKey, flight);
    }
    return signal ? raceAbort(flight, signal) : flight;
  }

  async #run(key: string, policy: "authority" | "metadata", fetch: () => Promise<string>, options: ReadOptions): Promise<string> {
    if (!options.bypass) {
      const cached = await this.#lookup(policy, key);
      if (cached && this.clock() - cached.stored < READ_CACHE_TTL_MS) return cached.body;
    }
    let body: string;
    try {
      body = await fetch();
    } catch (error) {
      if (isCancellation(error)) throw error;
      if (policy === "metadata" || (isTransient(error) && !options.bypass)) {
        const old = await this.#lookup(policy, key);
        if (old) return this.#stale(key, old, error, options);
      } else {
        this.memory.remove(key);
      }
      throw error;
    }
    try {
      if (policy === "metadata") await this.disk?.put(key, body, this.clock());
      else this.memory.put(key, body, this.clock());
    } catch (error) {
      this.log.warn("read cache store failed", { key, err: errorMessage(error) });
    }
    return body;
  }

  async #lookup(policy: "authority" | "metadata", key: string): Promise<Cached | undefined> {
    return policy === "metadata" ? this.disk?.get(key) : this.memory.get(key);
  }

  #stale(key: string, old: Cached, error: unknown, options: ReadOptions): string {
    options.onStale?.();
    this.log.warn("read cache stale fallback", { key, age_seconds: Math.round((this.clock() - old.stored) / 1000), err: errorMessage(error) });
    return old.body;
  }
}

function raceAbort<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(signal.reason);
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

/** Request identity: method, URL, every header (including credentials) and body, hashed. */
export function requestKey(request: HttpRequest): string {
  const headers = Object.entries(request.headers ?? {})
    .map(([name, value]) => [name.toLowerCase(), value])
    .sort((a, b) => (a[0]! < b[0]! ? -1 : a[0]! > b[0]! ? 1 : 0));
  return createHash("sha256")
    .update(JSON.stringify([request.method ?? "GET", request.url, headers, request.body ?? ""]))
    .digest("hex");
}

export interface JsonRead<T> {
  request: HttpRequest;
  policy: CachePolicy;
  /** Validates the body before it can be cached; throw to reject it. */
  decode: (text: string) => T;
}

export interface JsonResult<T> {
  value: T;
  /** The exact body the value came from (for change fingerprints). */
  text: string;
}

/** GET-like read: only HTTP 200 with a body that passes `decode` succeeds or is cached. */
export async function readJSON<T>(pool: HttpPool, cache: ReadCache | undefined, read: JsonRead<T>, options: ReadOptions = {}): Promise<JsonResult<T>> {
  let mine: JsonResult<T> | undefined;
  const fetch = async () => {
    const response = await pool.read({ ...read.request, signal: options.signal });
    if (response.status !== 200) throw new ReadStatusError(response.status);
    let value: T;
    try {
      value = read.decode(response.text);
    } catch (error) {
      if (error instanceof SyntaxError) throw new InvalidResponseError("invalid upstream JSON");
      throw error;
    }
    mine = { value, text: response.text };
    return response.text;
  };
  if (read.policy === "none" || !cache) {
    await fetch();
    return mine!;
  }
  const text = await cache.load(requestKey(read.request), read.policy, fetch, options);
  // The leader reuses its own decode; followers and cache hits decode once.
  return mine && mine.text === text ? mine : { value: read.decode(text), text };
}
