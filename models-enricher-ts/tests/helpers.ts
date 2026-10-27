// Test doubles: a loopback HTTP server, a capturing logger and private temp directories.
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AddressInfo } from "node:net";
import type { Logger } from "../src/log.ts";

export type Handler = (req: IncomingMessage & { body: string }, res: ServerResponse) => void | Promise<void>;

export interface TestServer {
  url: string;
  close(): Promise<void>;
}

export async function serve(handler: Handler): Promise<TestServer> {
  const server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      const withBody = Object.assign(req, { body: Buffer.concat(chunks).toString("utf8") });
      Promise.resolve(handler(withBody, res)).catch((error) => {
        res.statusCode = 500;
        res.end(String(error));
      });
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}

export function json(res: ServerResponse, body: unknown, status = 200): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(typeof body === "string" ? body : JSON.stringify(body));
}

export interface CapturedLog extends Logger {
  lines: string[];
  text(): string;
}

export function captureLog(): CapturedLog {
  const lines: string[] = [];
  const emit = (level: string) => (msg: string, fields?: object) => lines.push(JSON.stringify({ level, msg, ...fields }));
  return { lines, text: () => lines.join("\n"), info: emit("INFO"), warn: emit("WARN"), error: emit("ERROR") };
}

export function tempDir(): Promise<string> {
  return mkdtemp(join(tmpdir(), "models-enricher-test-"));
}

export const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
