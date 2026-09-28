#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""A minimal runner of the adapter protocol wbft-vector/1 (A-11 §3.4).

Starts an adapter command, exchanges hello, sends every case under the vector
directory (input.yaml converted to JSON, WBFT-VEC-040), one at a time
(WBFT-VEC-042), decides each case as WBFT-VEC-047 and WBFT-VEC-048 say, ends
with bye (WBFT-VEC-045) and prints the counts. It is the check of WBFT-VEC-062:
run it against the generator's adapter to check the vectors against their
generator.

Usage:
  python3 check_adapter.py VECTORS_DIR -- ADAPTER_COMMAND [ARGS...]
Example (from tools/vectorgen):
  GOTOOLCHAIN=go1.23.12 go build -o /tmp/vg-adapter ./adapter
  python3 check_adapter.py ../../vectors -- /tmp/vg-adapter
Requires: pyyaml. Exit code 1 if any case fails or is unsupported.
"""
import json, pathlib, subprocess, sys

import yaml


def main():
    if "--" not in sys.argv or sys.argv.index("--") != 2:
        print(__doc__)
        sys.exit(2)
    root = pathlib.Path(sys.argv[1])
    cmd = sys.argv[3:]
    p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1 << 20)

    def send(o):
        p.stdin.write(json.dumps(o, separators=(",", ":")) + "\n")
        p.stdin.flush()

    def recv():
        line = p.stdout.readline()
        if not line:
            raise SystemExit("adapter exited")
        return json.loads(line)

    send({"type": "hello", "protocol": "wbft-vector/1", "runner": {"name": "check_adapter", "version": "0.1.0"}, "spec_commit": "local"})
    h = recv()
    if h.get("type") != "hello" or h.get("protocol") != "wbft-vector/1":
        raise SystemExit(f"bad hello: {h}")
    if h.get("improvements"):
        print("diagnostic run: the adapter reports improvements", h["improvements"])
    supported = set(h["handlers"])
    counts = {"pass": 0, "fail": 0, "unsupported": 0}
    failures = []
    cid = 0
    for meta in sorted(root.glob("*/*/*/meta.yaml")):
        case = meta.parent
        m = yaml.safe_load(meta.read_text())
        runner, handler = m["runner"], m["handler"]
        if f"{runner}/{handler}" not in supported:
            counts["unsupported"] += 1
            continue
        cid += 1
        send({"type": "case", "id": cid, "runner": runner, "handler": handler, "case": m["case"], "kind": m["kind"],
              "input": yaml.safe_load((case / "input.yaml").read_text())})
        r = recv()
        if r.get("type") != "result" or r.get("id") != cid:
            raise SystemExit(f"protocol error at {case}: {r}")
        exp_file = case / "expected.yaml"
        if r["status"] == "unsupported":
            counts["unsupported"] += 1
            continue
        if exp_file.exists():
            ok = r["status"] == "ok" and r.get("output") == yaml.safe_load(exp_file.read_text())
        else:
            ok = r["status"] == "error"
        counts["pass" if ok else "fail"] += 1
        if not ok:
            failures.append(str(case.relative_to(root)))
    send({"type": "bye"})
    p.stdin.close()
    p.wait(timeout=5)
    print(f"pass={counts['pass']} fail={counts['fail']} unsupported={counts['unsupported']}")
    for f in failures:
        print("FAIL", f)
    sys.exit(1 if counts["fail"] or counts["unsupported"] else 0)


if __name__ == "__main__":
    main()
