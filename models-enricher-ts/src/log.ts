// Structured JSON-lines logger. Callers pass only non-secret fields (names, prefixes, status codes).

export type LogFields = Record<string, string | number | boolean | undefined>;

export interface Logger {
  info(msg: string, fields?: LogFields): void;
  warn(msg: string, fields?: LogFields): void;
  error(msg: string, fields?: LogFields): void;
}

export function jsonLogger(write: (line: string) => void = (line) => process.stdout.write(line)): Logger {
  const emit = (level: string) => (msg: string, fields?: LogFields) =>
    write(JSON.stringify({ time: new Date().toISOString(), level, msg, ...fields }) + "\n");
  return { info: emit("INFO"), warn: emit("WARN"), error: emit("ERROR") };
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
