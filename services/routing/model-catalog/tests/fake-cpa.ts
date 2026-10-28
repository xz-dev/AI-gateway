// Local CPA stand-in: native manifest, management channel lists, prefix endpoints and api-call.
import type { IncomingMessage, ServerResponse } from "node:http";
import { CHANNEL_KINDS, CpaClient } from "../src/cpa.ts";
import { HttpPool } from "../src/http.ts";
import { ReadCache } from "../src/read-cache.ts";
import { captureLog, json, serve, type CapturedLog, type TestServer } from "./helpers.ts";

export const MANAGEMENT_KEY = "management-secret-key";
export const CLIENT_KEY = "client-secret-key";

export interface FakeCPA {
  native: string;
  nativeStatus?: number;
  /** Body for GET /v0/management/openai-compatibility. */
  channels?: string;
  /** CPA's `force-model-prefix` setting; default false (the upstream default). */
  forceModelPrefix?: boolean;
  /** Prefix endpoints (auth-files, auth-files/models, force-model-prefix); default: no auth files. */
  identity?: (req: IncomingMessage & { body: string }, res: ServerResponse) => void;
  apiCall?: (payload: { method: string; url: string; header: Record<string, string>; data?: string; auth_index?: string }, res: ServerResponse) => void;
  calls: string[];
}

export function fakeCPA(init: Partial<FakeCPA> & { native: string }): FakeCPA {
  return { calls: [], ...init };
}

export async function serveCPA(fake: FakeCPA): Promise<TestServer> {
  return serve((req, res) => {
    const url = new URL(req.url!, "http://cpa");
    fake.calls.push(`${req.method} ${url.pathname}${url.search}`);
    if (url.pathname === "/v1/models") {
      if (req.headers.authorization !== `Bearer ${CLIENT_KEY}`) return json(res, {}, 401);
      return json(res, fake.native, fake.nativeStatus ?? 200);
    }
    if (req.headers.authorization !== `Bearer ${MANAGEMENT_KEY}`) return json(res, {}, 401);
    if (url.pathname === "/v0/management/api-call") {
      if (!fake.apiCall) return json(res, { status_code: 404, body: "" });
      return fake.apiCall(JSON.parse(req.body), res);
    }
    if (url.pathname.startsWith("/v0/management/auth-files") || url.pathname === "/v0/management/force-model-prefix") {
      if (fake.identity) return fake.identity(req, res);
      if (url.pathname === "/v0/management/force-model-prefix") return json(res, { "force-model-prefix": fake.forceModelPrefix ?? false });
      if (url.pathname === "/v0/management/auth-files") return json(res, { files: [] });
      return json(res, {}, 404);
    }
    const kind = url.pathname.slice("/v0/management/".length);
    if ((CHANNEL_KINDS as readonly string[]).includes(kind)) {
      if (kind === "openai-compatibility" && fake.channels) return json(res, fake.channels);
      return json(res, { [kind]: [] });
    }
    json(res, {}, 404);
  });
}

export function cpaClient(url: string, options: { cache?: ReadCache; log?: CapturedLog; concurrency?: number } = {}) {
  const log = options.log ?? captureLog();
  const cache = options.cache ?? new ReadCache(undefined, log);
  const client = new CpaClient({ baseUrl: url, managementKey: MANAGEMENT_KEY, clientKey: CLIENT_KEY, pool: new HttpPool(options.concurrency ?? 4, 5_000), cache, log });
  return { client, log, cache };
}
