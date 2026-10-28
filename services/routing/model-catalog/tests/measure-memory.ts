// Task 1.6 memory measurement (not part of `npm test`): 50 forced refreshes on the production-shaped
// fixture with the bundled workers and the real metadata tables; each round overlaps a Codex build
// with a membership collection. Run after `npm run build`, with the runtime flags of the image:
//   MALLOC_ARENA_MAX=2 node --max-old-space-size=96 --expose-gc tests/measure-memory.ts
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { replyText } from "./catalog-harness.ts";
import { fixtureHarness, forcedRows } from "./parity-fixture.ts";

const closers: Array<() => unknown> = [];
// The tracked production config: its chains read the metadata tables in the compile worker.
const yaml = (await readFile(new URL("../config.yaml", import.meta.url), "utf8")).replace(/^cpa_base_url:.*$/m, "");
const h = await fixtureHarness({ after: (fn) => closers.push(fn) }, "forced", { yaml });
(h.deps as { tablesPath: string }).tablesPath = fileURLToPath(new URL("../data/source-tables.json", import.meta.url));
h.deps.workerFile = fileURLToPath(new URL("../dist/worker.mjs", import.meta.url));
h.owner.options.membership = { workerFile: new URL("../dist/membership-worker.mjs", import.meta.url) };

const mib = (bytes: number) => Math.round((bytes / 2 ** 20) * 10) / 10;
let peak = 0;
const sampler = setInterval(() => (peak = Math.max(peak, process.memoryUsage.rss())), 5);
const native = (round: number) => JSON.stringify({ models: [...forcedRows, { slug: `devin/round-${round}`, display_name: "r" }] });

const settled: number[] = [];
const overlap: number[] = [];
await replyText(await h.service.catalog("1.0.0"));
for (let round = 0; round < 50; round++) {
  h.fake.native = native(round);
  await h.owner.refresh({ bypass: true });
  // The build for this generation runs while the next collection (membership worker) runs.
  const started = performance.now();
  let buildMs = 0;
  let collectionMs = 0;
  const build = h.service.catalog("1.0.0").then(replyText).then(() => (buildMs = performance.now() - started));
  h.fake.native = native(round + 1000);
  const collection = h.owner.refresh({ bypass: true }).then((outcome) => ((collectionMs = performance.now() - started), outcome));
  const [, outcome] = await Promise.all([build, collection]);
  if (outcome.generation.members.length > 0 && buildMs > 0 && collectionMs > 0) overlap.push(Math.min(buildMs, collectionMs));
  if (outcome.scriptError) throw outcome.scriptError;
  await new Promise((resolve) => setTimeout(resolve, 50));
  globalThis.gc?.();
  settled.push(process.memoryUsage.rss());
}
clearInterval(sampler);
const last = settled.slice(-10);
const slope = (last.at(-1)! - last[0]!) / (last.length - 1);
console.log(JSON.stringify({
  rounds: settled.length,
  peakMiB: mib(peak),
  settledFirstMiB: mib(settled[0]!),
  settledLast10MiB: last.map(mib),
  last10SlopeMiBPerRound: mib(slope),
  members: h.owner.current().members.length,
  overlapMsMedian: Math.round(overlap.sort((a, b) => a - b)[overlap.length >> 1]!),
}));
const msgs = new Map<string, number>();
for (const line of h.log.lines) { const msg = JSON.parse(line).msg as string; msgs.set(msg, (msgs.get(msg) ?? 0) + 1); }
console.log(JSON.stringify([...msgs]));
for (const close of closers.reverse()) await close();
process.exit(0);
