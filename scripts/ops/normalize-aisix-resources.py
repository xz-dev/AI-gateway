#!/usr/bin/env python3
"""Normalize aisix/resources.yaml before deploy.

Rule: every routing block with a non-empty `targets:` list gets
`max_fallbacks = len(targets) - 1`. Operators only edit the targets list;
the number is derived.

Text-level rewrite, byte-preserving outside touched lines:
  1. find `targets:` at indent I inside a routing block,
  2. count `- model:` items until block end (dedent / blank / next
     `- display_name:`),
  3. find the SINGLE `max_fallbacks:` line at indent I within that same
     routing block (it may sit before or after `targets:`),
  4. if it exists and its value is wrong -> rewrite in place; if it does
     not exist -> insert right after the last `- model:` line.
     Never inserts a second `max_fallbacks:` at indent I (YAML duplicated
     key = parse error).

Usage: normalize-aisix-resources.py RESOURCES.yaml
Prints `normalized N` (blocks rewritten). Exit 0 always.
"""
import re
import sys
from pathlib import Path

MODEL_RE = re.compile(r"^-\s+display_name:")
TARGETS_RE = re.compile(r"^(\s+)targets:\s*$")
ITEM_RE = re.compile(r"^\s+-\s+model:")
MF_RE = re.compile(r"^(\s+)max_fallbacks:\s*-?\d+\s*$")


def normalize(text: str) -> tuple[str, int]:
    out = text.splitlines(keepends=True)
    rewrites = 0
    i = 0
    while i < len(out):
        m = TARGETS_RE.match(out[i])
        if not m:
            i += 1
            continue
        indent = m.group(1)
        # block start = line above `targets:` that opens the routing mapping.
        # walk backwards until dedent (line starting at col 0 or `- display_name:`).
        start = i
        while start > 0:
            prev = out[start - 1]
            if prev.strip() == "" or MODEL_RE.match(prev):
                break
            start -= 1
        # scan routing block for items + existing max_fallbacks at indent I
        j = start
        count = 0
        mf_idx = None
        last_item = None
        targets_seen = False
        while j < len(out):
            line = out[j]
            if line.strip() == "" or MODEL_RE.match(line):
                break
            if ITEM_RE.match(line):
                count += 1
                last_item = j
            mm = MF_RE.match(line)
            if mm and mm.group(1) == indent:
                mf_idx = j
            if TARGETS_RE.match(line) and TARGETS_RE.match(line).group(1) == indent:
                targets_seen = True
            j += 1
        if not count or not targets_seen:
            i = j
            continue
        want = f"{indent}max_fallbacks: {count - 1}\n"
        if mf_idx is not None:
            if out[mf_idx] != want:
                out[mf_idx] = want
                rewrites += 1
        else:
            out.insert(last_item + 1, want)
            rewrites += 1
            j += 1
        i = j
    return "".join(out), rewrites


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {sys.argv[0]} RESOURCES.yaml")
    path = Path(sys.argv[1])
    new, n = normalize(path.read_text(encoding="utf-8"))
    if n:
        path.write_text(new, encoding="utf-8")
    print(f"normalized {n}")


if __name__ == "__main__":
    main()
