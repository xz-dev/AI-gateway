// JSON helpers shared by every module. Rows are plain parsed JSON objects.

export type Row = Record<string, unknown>;

declare global {
  interface JSON {
    rawJSON(text: string): unknown;
    isRawJSON(value: unknown): boolean;
  }
}

type ReviverContext = { source?: string };

// Integers beyond 2^53 keep their exact source text, so re-serialized CPA records stay byte-faithful.
function losslessReviver(this: unknown, _key: string, value: unknown, context?: ReviverContext): unknown {
  if (typeof value === "number" && !Number.isSafeInteger(value) && Number.isInteger(value) && context?.source) {
    return JSON.rawJSON(context.source);
  }
  return value;
}

// The reviver costs ~6x parse time, so it only runs when a 16+ digit run could be an unsafe integer.
export function parseJSON(text: string): unknown {
  if (!/[0-9]{16}/.test(text)) return JSON.parse(text);
  return JSON.parse(text, losslessReviver as (key: string, value: unknown) => unknown);
}

export function isRecord(value: unknown): value is Row {
  return typeof value === "object" && value !== null && !Array.isArray(value) && !JSON.isRawJSON(value);
}

export function asString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function toInt(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? Math.trunc(value) : 0;
}

// Deep copy of a JSON value; only used where a value is shared between two output rows.
export function cloneJSON<T>(value: T): T {
  if (Array.isArray(value)) return value.map(cloneJSON) as T;
  if (isRecord(value)) {
    const out: Row = {};
    for (const key of Object.keys(value)) out[key] = cloneJSON(value[key]);
    return out as T;
  }
  return value;
}

// Nested objects of `dst` may be shared with immutable source rows (entries are shallow copies), so
// both helpers copy each nested object they descend into instead of mutating it in place.

// Low-to-high layering: null never updates, objects merge, arrays and scalars replace.
export function overlay(dst: Row, src: Row | undefined): void {
  if (!src) return;
  for (const key of Object.keys(src)) {
    const value = src[key];
    if (value === null || value === undefined) continue;
    if (isRecord(value)) {
      const current: Row = isRecord(dst[key]) ? { ...(dst[key] as Row) } : {};
      overlay(current, value);
      dst[key] = current;
    } else {
      dst[key] = cloneJSON(value);
    }
  }
}

// RFC 7396 JSON Merge Patch; `protectedKeys` are never touched.
export function mergePatch(target: Row, patch: Row, protectedKeys: ReadonlySet<string> = new Set()): void {
  for (const key of Object.keys(patch)) {
    if (protectedKeys.has(key)) continue;
    const value = patch[key];
    if (value === null) {
      delete target[key];
    } else if (isRecord(value)) {
      const current: Row = isRecord(target[key]) ? { ...(target[key] as Row) } : {};
      mergePatch(current, value);
      target[key] = current;
    } else {
      target[key] = cloneJSON(value);
    }
  }
}

// The first trimmed-non-empty slug or id, with its original bytes.
export function exactModelID(row: Row): string {
  for (const field of ["slug", "id"]) {
    const value = row[field];
    if (typeof value === "string" && value.trim() !== "") return value;
  }
  return "";
}
