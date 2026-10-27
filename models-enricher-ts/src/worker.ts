// Compile worker entry: the extension's host-neutral body only. One worker per generation build;
// it posts the compiled catalog and is terminated, releasing the metadata tables with it.
import { parentPort, workerData } from "node:worker_threads";
import { runCompileWorkerBody } from "#core-worker";

if (parentPort) parentPort.postMessage(await runCompileWorkerBody(workerData));
