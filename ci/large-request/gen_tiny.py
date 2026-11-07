#!/usr/bin/env python3
"""Tiny-message corpus (review rr3 blocker 2): an exact-size Responses body of ~30-byte input items,
alternating user and assistant messages with a string content of one or two short words. The cost
of a converter is per item, so this is its worst shape.
usage: gen_tiny.py SIZE_MIB OUT"""
import json, os, random, sys

mib, out = int(sys.argv[1]), sys.argv[2]
target = mib * 2**20
rnd = random.Random(31)
words = "ok yes no go run fix log a b c".split()


def item(i):
    role = "user" if i % 2 == 0 else "assistant"
    t = " ".join(rnd.choice(words) for _ in range(rnd.randint(1, 2)))
    return json.dumps({"role": role, "content": t}, separators=(",", ":"))


head = '{"model":"gpt-5.1-codex","stream":true,"store":false,"instructions":"be brief","input":['
tail_tpl = '{"role":"user","content":"%s"}]}'
parts, size, i = [], len(head), 0
budget = target - 4096
while True:
    s = item(i)
    if size + len(s) + 1 > budget:
        break
    parts.append(s); size += len(s) + 1; i += 1
body_prefix = head + ",".join(parts) + ","
pad = target - len(body_prefix.encode()) - len(tail_tpl % "")
assert 0 <= pad, pad
body = body_prefix + tail_tpl % ("x" * pad)
with open(out, "w") as f:
    f.write(body)
assert os.path.getsize(out) == target, (os.path.getsize(out), target)
json.load(open(out))
print(out, target, "items", len(parts) + 1, "mean item bytes %.1f" % (size / len(parts)))
