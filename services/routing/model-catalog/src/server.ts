// HTTP surface on node:http. /routing-index was removed (no consumer) and answers 404 like any
// other unknown path; /models-table/refresh accepts POST only.
import { createReadStream } from "node:fs";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { pipeline } from "node:stream/promises";
import { errorMessage, type Logger } from "./log.ts";
import { errorReply, type CatalogService, type Reply } from "./service.ts";

/** Writes `body`, then streams the published file in 64 KiB chunks; the lease is always released. */
async function send(res: ServerResponse, reply: Reply): Promise<void> {
  const { file } = reply;
  try {
    if (res.writableEnded || res.destroyed) return;
    const length = Buffer.byteLength(reply.body) + (file ? file.size + Buffer.byteLength(file.tail) : 0);
    res.writeHead(reply.status, { "Content-Type": reply.contentType, "Content-Length": length });
    if (!file) return void res.end(reply.body);
    if (reply.body !== "") res.write(reply.body);
    await pipeline(createReadStream(file.path, { highWaterMark: 64 << 10 }), res, { end: false });
    res.end(file.tail);
  } catch {
    // Client gone or file unreadable after the headers: drop the connection, never a short body.
    res.destroy();
  } finally {
    file?.release();
  }
}

/** Aborts when the client goes away before the response is written. */
function clientSignal(req: IncomingMessage, res: ServerResponse): AbortSignal {
  const controller = new AbortController();
  res.on("close", () => {
    if (!res.writableFinished) controller.abort(new DOMException("client disconnected", "AbortError"));
  });
  req.resume();
  return controller.signal;
}

export function handler(service: CatalogService, log: Logger) {
  return async (req: IncomingMessage, res: ServerResponse): Promise<void> => {
    const url = new URL(req.url ?? "/", "http://model-catalog");
    const method = req.method ?? "GET";
    try {
      switch (url.pathname) {
        case "/healthz":
          return send(res, { status: 200, contentType: "text/plain", body: "ok" });
        case "/readyz": {
          if (method !== "GET") return send(res, errorReply(405, "method_not_allowed", "GET only"));
          const { ready, generation } = service.ready();
          return send(res, ready ? { status: 200, contentType: "application/json", body: JSON.stringify({ status: "ready", generation }) } : errorReply(503, "catalog_not_ready", "CPA catalog snapshot is not ready"));
        }
        case "/v1/models": {
          if (method !== "GET") return send(res, errorReply(405, "method_not_allowed", "GET only"));
          const signal = clientSignal(req, res);
          return send(res, await service.catalog(url.searchParams.get("client_version") ?? "", signal));
        }
        case "/models-table":
          if (method !== "GET") return send(res, errorReply(405, "method_not_allowed", "GET only"));
          return send(res, service.tablePage());
        case "/models-table/refresh": {
          if (method !== "POST") return send(res, errorReply(405, "method_not_allowed", "POST only"));
          const signal = clientSignal(req, res);
          return send(res, await service.refreshPage(signal));
        }
        default:
          return send(res, errorReply(404, "not_found", "not found"));
      }
    } catch (error) {
      if (res.destroyed || res.writableEnded) return; // client left; shared work continues
      log.error("request failed", { path: url.pathname, err: errorMessage(error) });
      send(res, errorReply(500, "internal_error", "request failed"));
    }
  };
}

export function createHTTPServer(service: CatalogService, log: Logger): Server {
  const handle = handler(service, log);
  return createServer((req, res) => void handle(req, res));
}

/** ":8090" → all interfaces, "127.0.0.1:8090" → that host. */
export function parseListenAddr(addr: string): { host?: string; port: number } {
  const index = addr.lastIndexOf(":");
  const port = Number(addr.slice(index + 1));
  if (index < 0 || !Number.isInteger(port) || port < 0 || port > 65535) throw new Error(`invalid LISTEN_ADDR ${JSON.stringify(addr)}`);
  const host = addr.slice(0, index).replace(/^\[|\]$/g, "");
  return host ? { host, port } : { port };
}
