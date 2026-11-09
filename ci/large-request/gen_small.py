#!/usr/bin/env python3
"""Structure-heavy corpus (follow-up 6, item 1): an exact-size Responses body made of many ~90-byte
input items: user messages with a string content, user messages with an input_text part, and short
assistant output_text messages. The guard reads every user text; the cost is per JSON value, not text.
usage: gen_small.py SIZE_MIB OUT"""
import json, os, random, sys

mib, out = int(sys.argv[1]), sys.argv[2]
target = mib * 2**20
rnd = random.Random(29)
words = "ok yes no run test build log fix diff file path retry step next done".split()


def text(n):
    return " ".join(rnd.choice(words) for _ in range(n))


def item(i):
    k = i % 3
    if k == 0:  # easy input message, ~90 bytes
        return json.dumps({"role": "user", "content": text(rnd.randint(8, 12))}, separators=(",", ":"))
    if k == 1:  # input item with an input_text part, ~90 bytes
        return json.dumps({"type": "message", "role": "user",
                           "content": [{"type": "input_text", "text": text(rnd.randint(2, 4))}]}, separators=(",", ":"))
    return json.dumps({"type": "message", "role": "assistant",
                       "content": [{"type": "output_text", "text": text(rnd.randint(1, 3))}]}, separators=(",", ":"))


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
