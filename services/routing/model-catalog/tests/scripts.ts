// Membership scripts for tests: the tracked default `catalog.lua`, optionally with an extra CPA
// list and a hide list (the production profile's shape, design Migration Plan step 2).
import { readFileSync } from "node:fs";
import { membershipScript, type MembershipScript } from "../src/membership.ts";

export const DEFAULT_SCRIPT_TEXT = readFileSync(new URL("../catalog.lua", import.meta.url), "utf8");
const EXTRA_LINE = "local EXTRA_CPA = {}\n";
if (!DEFAULT_SCRIPT_TEXT.includes(EXTRA_LINE)) throw new Error("catalog.lua no longer declares an empty EXTRA_CPA");

const lua = (globs: readonly string[]) => `{ ${globs.map((glob) => JSON.stringify(glob)).join(", ")} }`;

/** The default script with `EXTRA_CPA` set, and members matching `hide` rejected as "hidden by profile". */
export function defaultScriptText(extra: readonly string[] = [], hide: readonly string[] = []): string {
  let text = DEFAULT_SCRIPT_TEXT.replace(EXTRA_LINE, `local EXTRA_CPA = ${lua(extra)}\n`);
  if (hide.length > 0) {
    text += `
local HIDE = ${lua(hide)}
local base = select
function select(m)
  for _, rule in ipairs(HIDE) do
    if glob(rule, m.id) then return false, "hidden by profile" end
  end
  return base(m)
end
`;
  }
  return text;
}

export const script = (text: string): MembershipScript => membershipScript(Buffer.from(text));
export const defaultScript = (extra: readonly string[] = [], hide: readonly string[] = []) => script(defaultScriptText(extra, hide));
