// Bundles the server, the compile worker and the membership worker as separate ESM files (each
// worker runs in its own thread and must not pull in the server graph). `yaml` calls
// require("process") internally and wasmoon's CommonJS build reads __filename, so ESM output
// needs CommonJS shims. wasmoon's glue.wasm ships next to the bundles (no runtime fetch).
import { copyFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { build } from "esbuild";

await build({
  entryPoints: { server: "src/main.ts", worker: "src/worker.ts", "membership-worker": "src/membership-worker.ts" },
  outdir: "dist",
  outExtension: { ".js": ".mjs" },
  bundle: true,
  platform: "node",
  format: "esm",
  target: "node26",
  legalComments: "none",
  banner: {
    js: [
      'import { createRequire as __createRequire } from "node:module";',
      'import { fileURLToPath as __fileURLToPath } from "node:url";',
      "const require = __createRequire(import.meta.url);",
      "const __filename = __fileURLToPath(import.meta.url);",
    ].join(" "),
  },
  logLevel: "warning",
});
await copyFile(createRequire(import.meta.url).resolve("wasmoon/dist/glue.wasm"), "dist/glue.wasm");
