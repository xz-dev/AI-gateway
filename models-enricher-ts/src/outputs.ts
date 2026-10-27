// Published response bodies on disk (design D6). Each body is written once per build to
// <CACHE_DIR>/out/<generation>-<seq>-<key hash> and streamed from there; process memory holds only
// this index. A replaced file is unlinked once no response still streams it. The directory is
// separate from the v2/ input cache and never counts against its budget.
import { createHash } from "node:crypto";
import { mkdir, readdir, rename, rm, unlink, writeFile } from "node:fs/promises";
import { join } from "node:path";

/** Codex manifests are rebuilt in the background once older than this (@cpa-upstream freshness). */
export const PROJECTION_TTL_MS = 5 * 60_000;
/** Bound on distinct published keys (client versions plus the standard list and the table). */
export const OUTPUT_MAX_ENTRIES = 24;

export interface Published {
  readonly key: string;
  readonly generation: number;
  readonly path: string;
  readonly size: number;
  /** Epoch ms of the build that wrote the file. */
  readonly stored: number;
  /** Small per-body metadata (the table summary); never a body. */
  readonly meta?: unknown;
  lastRead: number;
  readers: number;
  retired: boolean;
}

export interface Lease {
  readonly path: string;
  readonly size: number;
  readonly generation: number;
  readonly meta?: unknown;
  release(): void;
}

export class OutputStore {
  readonly dir: string;
  /** Keys never evicted by the entry bound (the standard list and the table). */
  readonly pinned: ReadonlySet<string>;
  readonly #index = new Map<string, Published>();
  #seq = 0;

  private constructor(dir: string, pinned: ReadonlySet<string>) {
    this.dir = dir;
    this.pinned = pinned;
  }

  /** Files left by a previous process are never referenced again, so the directory starts empty. */
  static async open(dir: string, pinned: ReadonlySet<string> = new Set()): Promise<OutputStore> {
    await mkdir(dir, { recursive: true, mode: 0o700 });
    for (const name of await readdir(dir)) await rm(join(dir, name), { recursive: true, force: true });
    return new OutputStore(dir, pinned);
  }

  get size(): number {
    return this.#index.size;
  }

  get(key: string): Published | undefined {
    return this.#index.get(key);
  }

  entries(): IterableIterator<Published> {
    return this.#index.values();
  }

  /** Keys read since `since`, most recently read first, within the entry bound. */
  activeKeys(since: number, skip: ReadonlySet<string>): string[] {
    return [...this.#index.values()]
      .filter((entry) => !skip.has(entry.key) && entry.lastRead >= since)
      .sort((a, b) => b.lastRead - a.lastRead)
      .slice(0, OUTPUT_MAX_ENTRIES)
      .map((entry) => entry.key);
  }

  /**
   * Writes `body` (tmp + rename) and makes it current for `key`, unless a newer generation already
   * published that key. The body string is not retained.
   */
  async publish(key: string, generation: number, body: string, stored: number, meta?: unknown): Promise<boolean> {
    if ((this.#index.get(key)?.generation ?? 0) > generation) return false;
    const name = `${generation}-${++this.#seq}-${createHash("sha256").update(key).digest("hex").slice(0, 16)}`;
    const path = join(this.dir, name);
    const payload = Buffer.from(body, "utf8");
    await writeFile(`${path}.tmp`, payload, { mode: 0o600 });
    try {
      await rename(`${path}.tmp`, path);
    } catch (error) {
      await unlink(`${path}.tmp`).catch(() => undefined);
      throw error;
    }
    const old = this.#index.get(key);
    if ((old?.generation ?? 0) > generation) {
      // A newer generation published while this file was written.
      await unlink(path).catch(() => undefined);
      return false;
    }
    this.#index.set(key, { key, generation, path, size: payload.length, stored, meta, lastRead: old?.lastRead ?? stored, readers: 0, retired: false });
    if (old) this.#retire(old);
    this.#evict(key);
    return true;
  }

  /** Retires entries older than `generation` that the caller did not rebuild. */
  retireOlder(generation: number, keep: ReadonlySet<string>): void {
    for (const entry of [...this.#index.values()]) if (entry.generation < generation && !keep.has(entry.key)) this.#drop(entry);
  }

  /** Synchronously pins the current file of `key` for one response. */
  lease(key: string, now: number): Lease | undefined {
    const entry = this.#index.get(key);
    if (!entry) return undefined;
    entry.readers++;
    entry.lastRead = now;
    let released = false;
    return {
      path: entry.path,
      size: entry.size,
      generation: entry.generation,
      meta: entry.meta,
      release: () => {
        if (released) return;
        released = true;
        entry.readers--;
        if (entry.retired && entry.readers === 0) void unlink(entry.path).catch(() => undefined);
      },
    };
  }

  #drop(entry: Published): void {
    if (this.#index.get(entry.key) === entry) this.#index.delete(entry.key);
    this.#retire(entry);
  }

  #retire(entry: Published): void {
    entry.retired = true;
    if (entry.readers === 0) void unlink(entry.path).catch(() => undefined);
  }

  /** Drops the least recently read entries beyond the bound, never the one just written. */
  #evict(keep: string): void {
    if (this.#index.size <= OUTPUT_MAX_ENTRIES) return;
    const victims = [...this.#index.values()].filter((entry) => entry.key !== keep && !this.pinned.has(entry.key)).sort((a, b) => a.lastRead - b.lastRead);
    for (const entry of victims) {
      if (this.#index.size <= OUTPUT_MAX_ENTRIES) return;
      this.#drop(entry);
    }
  }
}
