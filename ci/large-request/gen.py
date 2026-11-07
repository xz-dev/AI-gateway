#!/usr/bin/env python3
"""Shared capacity corpus (design D10): exact-size request bodies, deterministic per (shape, size, api, ws).

usage: gen.py SHAPE BYTES OUT [--api responses|chat] [--ws] [--model M] [--seed S]
  SHAPE image  media parts (base64 data-URL input_image, each >= 64 KiB) plus a FIXED inline part
               of INLINE_FIXED bytes at any size >= 4*INLINE_FIXED; smaller bodies use BYTES//4
               (a 1 MiB body cannot hold a 3 MiB inline part). Inline is always <= 4 MiB.
  SHAPE text   only strings <= 16 KiB (function_call / function_call_output / input_text);
               more than 8192 items at 32 MiB and above.
  --api chat   /v1/chat/completions messages instead of a Responses input (text shape only).
  --ws         the body starts with "type":"response.create" (one WS frame of exactly BYTES).
Every body is exactly BYTES bytes and valid JSON. A JSON summary goes to stdout:
bytes, inline_bytes (bytes outside allow-listed media strings >= 64 KiB), items, media_parts, sha256."""
import argparse, base64, hashlib, json, os, random

KIB, MIB = 1024, 1024 * 1024
INLINE_FIXED = 3 * MIB          # fixed inline part of the image shape (<= 4 MiB, D10)
MEDIA_T = 64 * KIB              # tier-A threshold T (D2)
IMG_B64_MAX = 8 * MIB           # base64 chars per image part
STR_MAX = 16 * KIB              # text shape: every string <= 16 KiB
WORDS = ("the keeper spool frame bytes request response tool output install driver virtio guest windows "
         "registry service log line error retry build test patch diff file path quote \"x\" tab\t uni\u00e9").split(" ")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("shape", choices=["image", "text"])
    ap.add_argument("bytes", type=int)
    ap.add_argument("out")
    ap.add_argument("--api", choices=["responses", "chat"], default="responses")
    ap.add_argument("--ws", action="store_true")
    ap.add_argument("--model", default="gpt-5.1-codex")
    ap.add_argument("--seed", type=int, default=11)
    a = ap.parse_args()
    assert not (a.api == "chat" and (a.shape == "image" or a.ws)), "chat: text shape over HTTP only"
    rnd = random.Random(a.seed * 1000003 + a.bytes)

    def text(n):  # ASCII-escaped JSON-safe text whose json.dumps() is about n bytes
        s, l = [], 0
        while l < n:
            w = rnd.choice(WORDS); s.append(w); l += len(w) + 1
        return " ".join(s)[:n]

    def jstr(n):  # a JSON string literal of at most n encoded bytes (incl. quotes)
        t = text(max(0, n - 2))
        e = json.dumps(t)
        while len(e) > n:
            t = t[:-1 - (len(e) - n)]; e = json.dumps(t)
        return e

    target = a.bytes
    pre = '{"type":"response.create",' if a.ws else "{"
    if a.api == "chat":
        head = pre + '"model":%s,"stream":true,"messages":[' % json.dumps(a.model)
        item = lambda s: '{"role":"user","content":%s}' % s
        tail_tpl = '{"role":"user","content":"%s"}]}'
    else:
        head = pre + '"model":%s,"stream":true,"store":false,"instructions":%s,"input":[' % (
            json.dumps(a.model), jstr(8000))
        tail_tpl = '{"type":"message","role":"user","content":[{"type":"input_text","text":"%s"}]}]}'
    tail_min = len(tail_tpl % "")
    parts, size, items, media = [], len(head), 0, []

    def add(p, n_items=1):
        nonlocal size, items
        parts.append(p); size += len(p) + 1; items += n_items

    inline_goal = (min(INLINE_FIXED, target // 4) if a.shape == "image" else target) - tail_min - STR_MAX
    i = 0
    while True:  # inline: tool call / output pairs, every string <= 16 KiB
        if a.api == "chat":
            p = item(jstr(rnd.randint(1000, 8000)))
            if size + len(p) + 1 > inline_goal: break
            add(p); continue
        cid = "call_%07d" % i
        arg = json.dumps(json.dumps({"command": text(rnd.randint(40, 400))}))
        c = '{"type":"function_call","call_id":"%s","name":"bash","arguments":%s}' % (cid, arg)
        o = '{"type":"function_call_output","call_id":"%s","output":%s}' % (cid, jstr(rnd.randint(1000, 8000)))
        if size + len(c) + len(o) + 2 > inline_goal: break
        add(c); add(o); i += 1
    if a.shape == "image":  # media fills the rest, leaving < 16 KiB for the padding item
        img_tpl = ('{"type":"message","role":"user","content":[{"type":"input_text","text":"screenshot %d"},'
                   '{"type":"input_image","image_url":"data:image/png;base64,%s"}]}')
        k = 0
        while True:
            room = target - size - tail_min - 1 - len(img_tpl % (k, ""))
            if room < MEDIA_T + STR_MAX: break
            if room > IMG_B64_MAX + MEDIA_T + STR_MAX: n = IMG_B64_MAX
            elif room > IMG_B64_MAX: n = room // 2          # split so both parts stay >= 64 KiB
            else: n = room - STR_MAX // 2                     # last part leaves ~8 KiB for padding
            n -= n % 4
            media.append((k, n)); size += len(img_tpl % (k, "")) + n + 1; items += 1; k += 1
    else:
        while (rem := target - size - tail_min) > STR_MAX:  # <= 15000-char items until < 16 KiB is left
            add(tail_tpl[:-2] % ("y" * min(15000, rem - STR_MAX // 2 - tail_min - 1)))
    pad = target - size - tail_min
    assert 0 <= pad <= STR_MAX, pad
    h = hashlib.sha256()
    with open(a.out, "w", encoding="ascii") as f:
        def w(s):
            f.write(s); h.update(s.encode())
        w(head + ",".join(parts) + ("," if parts else ""))
        for k, n in media:
            raw = rnd.randbytes(n * 3 // 4)
            s = img_tpl % (k, base64.b64encode(raw).decode())
            assert len(s) == len(img_tpl % (k, "")) + n
            w(s + ",")
        w(tail_tpl % ("x" * pad))
    got = os.path.getsize(a.out)
    assert got == target, (got, target)
    media_bytes = sum(n + len("data:image/png;base64,") for _, n in media)
    inline = target - media_bytes
    if a.shape == "image":
        assert inline <= 4 * MIB, inline
        if target >= 245_000_000: assert inline < 8 * MIB, inline  # 245 MB, 256 MiB, 1 GiB rungs
    elif target >= 32 * MIB:
        assert items + 1 > 8192, items
    if target <= 64 * MIB: json.load(open(a.out))
    print(json.dumps({"out": a.out, "shape": a.shape, "api": a.api, "ws": a.ws, "bytes": got,
                      "inline_bytes": inline, "items": items + 1, "media_parts": len(media),
                      "sha256": h.hexdigest()}))


if __name__ == "__main__":
    main()
