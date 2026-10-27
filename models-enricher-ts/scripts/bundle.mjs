// Bundles the server and the compile worker as separate ESM files (the worker runs in its own
// thread and must not pull in the server graph). `yaml` calls require("process") internally,
// so ESM output needs a CommonJS require shim.
import { build } from "esbuild";

await build({
  entryPoints: { server: "src/main.ts", worker: "src/worker.ts" },
  outdir: "dist",
  outExtension: { ".js": ".mjs" },
  bundle: true,
  platform: "node",
  format: "esm",
  target: "node26",
  legalComments: "none",
  banner: { js: 'import { createRequire as __createRequire } from "node:module"; const require = __createRequire(import.meta.url);' },
  logLevel: "warning",
});
