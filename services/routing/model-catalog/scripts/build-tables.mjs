// Build step: converts the exactly pinned data packages to the SourceTables JSON the compile worker
// reads at runtime, so the image ships neither the packages nor a runtime network dependency.
// Usage: node scripts/build-tables.mjs [output] (default: data/source-tables.json)
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { buildSourceTables } from "#core";

// The packages do not export ./package.json; resolve them under this package's node_modules.
const modules = resolve(import.meta.dirname, "..", "node_modules");
const root = (name) => join(modules, name);
const load = async (name, entry) => {
  const dir = root(name);
  const version = JSON.parse(readFileSync(join(dir, "package.json"), "utf8")).version;
  return { module: await import(pathToFileURL(join(dir, entry)).href), version };
};

const output = resolve(process.argv[2] ?? "data/source-tables.json");
const tables = buildSourceTables({
  neededSites: ["models.dev", "modelparams.dev"],
  modelsDev: await load("@opencode-ai/models", "dist/snapshot.js"),
  modelparams: await load("modelparams", "dist/index.js"),
});
if (tables.degraded.length > 0) {
  process.stderr.write(`source tables degraded: ${JSON.stringify(tables.degraded)}\n`);
  process.exit(1);
}
mkdirSync(dirname(output), { recursive: true });
writeFileSync(output, JSON.stringify(tables));
process.stdout.write(`${JSON.stringify({ output, modelsDevVersion: tables.modelsDevVersion, modelsDevGeneratedAt: tables.modelsDevGeneratedAt, modelparamsVersion: tables.modelparamsVersion })}\n`);
