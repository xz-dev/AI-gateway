// Bounded outbound HTTP: one shared concurrency limit, a per-request timeout and a body-size cap.

export class ReadStatusError extends Error {
  readonly status: number;

  constructor(status: number) {
    super(`upstream status ${status}`);
    this.status = status;
  }
}

export class InvalidResponseError extends Error {}

export interface HttpRequest {
  url: string;
  method?: string;
  headers?: Record<string, string>;
  body?: string;
  signal?: AbortSignal;
  /** Maximum accepted body size in bytes. */
  limit: number;
}

export interface HttpResponse {
  status: number;
  text: string;
}

// Deadlines (ours or the caller's AbortSignal.timeout) and network failures may reuse stale data;
// a plain abort is a cancellation and never does.
export function isCancellation(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

export function isDeadline(error: unknown): boolean {
  return error instanceof Error && error.name === "TimeoutError";
}

export function isTransient(error: unknown): boolean {
  if (error instanceof ReadStatusError) return error.status === 502 || error.status === 503 || error.status === 504;
  if (isDeadline(error)) return true;
  // fetch reports connection failures as TypeError("fetch failed"); a cut body surfaces as a socket error.
  if (error instanceof TypeError) return true;
  const code = (error as { code?: unknown } | null)?.code;
  return typeof code === "string" && /^(ECONN|EPIPE|ETIMEDOUT|EAI_|UND_ERR_)/.test(code);
}

export class HttpPool {
  #active = 0;
  readonly #waiters: Array<() => void> = [];

  readonly concurrency: number;
  readonly timeoutMs: number;

  constructor(concurrency: number, timeoutMs: number) {
    if (!(concurrency > 0)) throw new Error("http concurrency must be positive");
    this.concurrency = concurrency;
    this.timeoutMs = timeoutMs;
  }

  async read(request: HttpRequest): Promise<HttpResponse> {
    await this.#acquire(request.signal);
    try {
      const signal = request.signal
        ? AbortSignal.any([request.signal, AbortSignal.timeout(this.timeoutMs)])
        : AbortSignal.timeout(this.timeoutMs);
      const response = await fetch(request.url, {
        method: request.method ?? "GET",
        headers: request.headers,
        body: request.body,
        signal,
        // Never follow redirects with credentials attached.
        redirect: "manual",
      });
      if (response.status !== 200) {
        await response.body?.cancel().catch(() => undefined);
        return { status: response.status, text: "" };
      }
      return { status: response.status, text: await readBounded(response, request.limit) };
    } finally {
      this.#release();
    }
  }

  #acquire(signal?: AbortSignal): Promise<void> {
    signal?.throwIfAborted();
    if (this.#active < this.concurrency) {
      this.#active++;
      return Promise.resolve();
    }
    return new Promise((resolve, reject) => {
      const grant = () => {
        signal?.removeEventListener("abort", onAbort);
        this.#active++;
        resolve();
      };
      const onAbort = () => {
        const index = this.#waiters.indexOf(grant);
        if (index >= 0) this.#waiters.splice(index, 1);
        reject(signal?.reason);
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      this.#waiters.push(grant);
    });
  }

  #release(): void {
    this.#active--;
    this.#waiters.shift()?.();
  }
}

async function readBounded(response: Response, limit: number): Promise<string> {
  const declared = Number(response.headers.get("content-length"));
  if (declared > limit) {
    await response.body?.cancel().catch(() => undefined);
    throw new InvalidResponseError(`response exceeds ${limit}-byte limit`);
  }
  if (!response.body) return "";
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > limit) {
      await reader.cancel().catch(() => undefined);
      throw new InvalidResponseError(`response exceeds ${limit}-byte limit`);
    }
    chunks.push(value);
  }
  return Buffer.concat(chunks, size).toString("utf8");
}
