#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The wbft-spec authors
# SPDX-License-Identifier: LGPL-3.0-or-later
"""check_yaml_subset: reject vector files outside the YAML subset of A-11.

The vector files (A-11 section 3.1, WBFT-VEC-013 .. WBFT-VEC-016) use a YAML
subset that converts to JSON one to one. This checker parses that subset with
its own strict parser (standard library only, no PyYAML), so that a file which
a general YAML parser would accept but which uses another construct (a comment,
a plain scalar, a number, an anchor, a flow collection that is not empty, ...)
is reported instead of silently interpreted.

Checks
  file level (WBFT-VEC-013)
    - UTF-8, LF line ends, final newline, no tab, no trailing space, no blank line
    - block mappings and block sequences indented by exactly two spaces
    - keys match [a-z0-9_]+, no duplicate key in a mapping
    - scalars are only: a JSON double-quoted string, true, false, null, [], {}
    - the document is a mapping
  value level (WBFT-VEC-014)
    - a string that starts with "0x" is lowercase hex with an even number of digits
  case level (WBFT-VEC-015, WBFT-VEC-016, WBFT-VEC-010)
    - layout vectors/<runner>/<handler>/<case>/, names match [a-z0-9_]+
    - a case holds meta.yaml and input.yaml, optionally expected.yaml, nothing else
    - meta.yaml has the required fields; runner/handler/case equal the directory names
    - expected_error only in a case without expected.yaml

Usage
  python3 check_yaml_subset.py <vectors dir> [...]     exit 1 on any problem
  python3 check_yaml_subset.py --self-test             run the built-in negative tests

Example
  python3 tools/vectorgen/check_yaml_subset.py vectors
"""
import json
import os
import re
import sys

KEY = re.compile(r"[a-z0-9_]+")
NAME = re.compile(r"[a-z0-9_]+")
HEX = re.compile(r"0x(?:[0-9a-f]{2})*")
REQ_ID = re.compile(r"(?:WBFT|SNET)-[A-Z]+-\d{3}")
CASE_FILES = {"meta.yaml", "input.yaml", "expected.yaml"}
META_TOP = ["runner", "handler", "case", "kind", "description", "requirements", "reference", "generator"]
META_REF = ["implementation", "commit", "toolchain", "build"]
META_GEN = ["name", "version"]
KINDS = {"pure", "steps", "chain"}


class SubsetError(Exception):
    pass


def parse_scalar(tok, lineno):
    """Return (True, value) if tok is an allowed scalar, else (False, None)."""
    if tok in ("true", "false", "null"):
        return True, {"true": True, "false": False, "null": None}[tok]
    if tok == "[]":
        return True, []
    if tok == "{}":
        return True, {}
    if tok.startswith('"'):
        try:
            val = json.loads(tok)
        except ValueError:
            raise SubsetError(f"line {lineno}: malformed double-quoted string")
        if not isinstance(val, str):
            raise SubsetError(f"line {lineno}: not a string")
        return True, val
    return False, None


class Parser:
    def __init__(self, text):
        if text.startswith("\ufeff"):
            raise SubsetError("byte order mark")
        if "\r" in text:
            raise SubsetError("CR character (use LF line ends)")
        if not text.endswith("\n"):
            raise SubsetError("no final newline")
        self.lines = []
        for i, raw in enumerate(text[:-1].split("\n"), 1):
            if "\t" in raw:
                raise SubsetError(f"line {i}: tab character")
            if raw.strip() == "":
                raise SubsetError(f"line {i}: blank line")
            if raw != raw.rstrip(" "):
                raise SubsetError(f"line {i}: trailing space")
            content = raw.lstrip(" ")
            indent = len(raw) - len(content)
            if indent % 2:
                raise SubsetError(f"line {i}: indentation is not a multiple of two spaces")
            self.lines.append([indent, content, i])
        self.pos = 0

    def peek(self):
        return self.lines[self.pos] if self.pos < len(self.lines) else None

    def parse_document(self):
        if not self.lines:
            raise SubsetError("empty document")
        first = self.peek()
        if first[0] != 0 or first[1].startswith("-"):
            raise SubsetError("the document is not a mapping at indentation 0")
        doc = self.parse_map(0)
        if self.pos != len(self.lines):
            _, _, ln = self.lines[self.pos]
            raise SubsetError(f"line {ln}: unexpected indentation")
        return doc

    def parse_block(self, indent, lineno):
        nxt = self.peek()
        if nxt is None or nxt[0] != indent:
            raise SubsetError(f"line {lineno}: empty block (write [] or {{}}) or wrong indentation")
        if nxt[1] == "-" or nxt[1].startswith("- "):
            return self.parse_seq(indent)
        return self.parse_map(indent)

    def parse_map(self, indent):
        out = {}
        while True:
            cur = self.peek()
            if cur is None or cur[0] < indent:
                return out
            ind, content, ln = cur
            if ind > indent:
                raise SubsetError(f"line {ln}: unexpected indentation")
            if content == "-" or content.startswith("- "):
                raise SubsetError(f"line {ln}: sequence item inside a mapping")
            m = re.fullmatch(r"([^:\s]+):(?: (.*))?", content)
            if not m:
                raise SubsetError(f"line {ln}: not a 'key: value' or 'key:' line")
            key, val = m.group(1), m.group(2)
            if not KEY.fullmatch(key):
                raise SubsetError(f"line {ln}: key {key!r} is not [a-z0-9_]+ (quoted keys are not in the subset)")
            if key in out:
                raise SubsetError(f"line {ln}: duplicate key {key!r}")
            self.pos += 1
            if val is None:
                out[key] = self.parse_block(indent + 2, ln)
            else:
                ok, v = parse_scalar(val, ln)
                if not ok:
                    raise SubsetError(f"line {ln}: value {val[:40]!r} is not a double-quoted string, true, false, null, [] or {{}}")
                out[key] = v

    def parse_seq(self, indent):
        out = []
        while True:
            cur = self.peek()
            if cur is None or cur[0] < indent:
                return out
            ind, content, ln = cur
            if ind > indent:
                raise SubsetError(f"line {ln}: unexpected indentation")
            if content == "-":
                self.pos += 1
                out.append(self.parse_block(indent + 2, ln))
            elif content.startswith("- "):
                rest = content[2:]
                ok, v = parse_scalar(rest, ln)
                if ok:
                    self.pos += 1
                    out.append(v)
                elif re.match(r"[^:\s\"]+:(?: |$)", rest):
                    # "- key: ..." starts a mapping item whose keys are aligned two columns right
                    cur[0], cur[1] = indent + 2, rest
                    out.append(self.parse_map(indent + 2))
                else:
                    raise SubsetError(f"line {ln}: item {rest[:40]!r} is not an allowed scalar or mapping")
            else:
                raise SubsetError(f"line {ln}: mapping key inside a sequence")


def parse(text):
    return Parser(text).parse_document()


def check_values(v, where, problems):
    if isinstance(v, dict):
        for k, x in v.items():
            check_values(x, f"{where}.{k}", problems)
    elif isinstance(v, list):
        for i, x in enumerate(v):
            check_values(x, f"{where}[{i}]", problems)
    elif isinstance(v, str) and v.startswith("0x") and not HEX.fullmatch(v):
        problems.append(f"{where}: byte string {v[:40]!r} is not lowercase even-length hex (WBFT-VEC-014)")


def check_meta(meta, runner, handler, case, has_expected, where, problems):
    for f in META_TOP:
        if f not in meta:
            problems.append(f"{where}: missing field {f!r} (WBFT-VEC-015)")
    for f, want in (("runner", runner), ("handler", handler), ("case", case)):
        if f in meta and meta[f] != want:
            problems.append(f"{where}: {f} = {meta[f]!r}, directory says {want!r}")
    if "kind" in meta and meta["kind"] not in KINDS:
        problems.append(f"{where}: kind {meta['kind']!r} is not one of {sorted(KINDS)}")
    reqs = meta.get("requirements")
    if not isinstance(reqs, list) or not reqs or not all(isinstance(r, str) and REQ_ID.fullmatch(r) for r in reqs):
        problems.append(f"{where}: requirements must be a non-empty list of requirement IDs")
    ref = meta.get("reference")
    if not isinstance(ref, dict) or any(f not in ref for f in META_REF):
        problems.append(f"{where}: reference must have {META_REF}")
    elif not re.fullmatch(r"[0-9a-f]{40}", str(ref["commit"])):
        problems.append(f"{where}: reference.commit is not a full lowercase commit hash")
    gen = meta.get("generator")
    if not isinstance(gen, dict) or any(f not in gen for f in META_GEN):
        problems.append(f"{where}: generator must have {META_GEN}")
    if "expected_error" in meta and has_expected:
        problems.append(f"{where}: expected_error in a case that has expected.yaml")


def check_tree(root):
    problems = []
    files = 0
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        rel = os.path.relpath(dirpath, root)
        parts = [] if rel == "." else rel.split(os.sep)
        for d in dirnames:
            if not NAME.fullmatch(d):
                problems.append(f"{os.path.join(rel, d)}: directory name is not [a-z0-9_]+ (WBFT-VEC-016)")
        if len(parts) < 3:
            if filenames:
                problems.append(f"{rel}: files outside a case directory: {sorted(filenames)}")
            continue
        if len(parts) > 3 or dirnames:
            problems.append(f"{rel}: directory below a case directory")
            continue
        runner, handler, case = parts
        names = set(filenames)
        if names - CASE_FILES:
            problems.append(f"{rel}: files not part of a case: {sorted(names - CASE_FILES)}")
        for need in ("meta.yaml", "input.yaml"):
            if need not in names:
                problems.append(f"{rel}: missing {need}")
        docs = {}
        for fn in sorted(names & CASE_FILES):
            path = os.path.join(dirpath, fn)
            files += 1
            try:
                text = open(path, encoding="utf-8").read()
                docs[fn] = parse(text)
            except UnicodeDecodeError:
                problems.append(f"{rel}/{fn}: not UTF-8")
                continue
            except SubsetError as e:
                problems.append(f"{rel}/{fn}: {e}")
                continue
            if fn != "meta.yaml":
                check_values(docs[fn], f"{rel}/{fn}", problems)
        if "meta.yaml" in docs:
            check_meta(docs["meta.yaml"], runner, handler, case, "expected.yaml" in names, f"{rel}/meta.yaml", problems)
    return files, problems


BAD_SAMPLES = {
    "comment": 'a: "1"  # c\n',
    "plain scalar": "a: 1\n",
    "single quotes": "a: '1'\n",
    "flow list": 'a: ["1"]\n',
    "anchor": 'a: &x "1"\n',
    "duplicate key": 'a: "1"\na: "2"\n',
    "tab": 'a:\n\t- "1"\n',
    "no final newline": 'a: "1"',
    "blank line": 'a: "1"\n\nb: "2"\n',
    "empty block": "a:\nb: null\n",
    "odd indent": 'a:\n   b: "1"\n',
    "document separator": '---\na: "1"\n',
    "top-level list": '- "1"\n',
    "uppercase key": 'A: "1"\n',
}
GOOD_SAMPLE = 'a: "0x00"\nb:\n  - "1"\n  - k: null\n    l: []\n  -\n    - true\nc:\n  d: {}\n'


def self_test():
    ok = True
    for name, text in BAD_SAMPLES.items():
        try:
            parse(text)
            print(f"self-test FAIL: {name} accepted")
            ok = False
        except SubsetError:
            pass
    got = parse(GOOD_SAMPLE)
    want = {"a": "0x00", "b": ["1", {"k": None, "l": []}, [True]], "c": {"d": {}}}
    if got != want:
        print(f"self-test FAIL: good sample parsed as {got}")
        ok = False
    probs = []
    check_values({"x": "0xABCD", "y": "0xabc"}, "t", probs)
    if len(probs) != 2:
        print("self-test FAIL: hex value check")
        ok = False
    print("self-test:", "ok" if ok else "FAILED")
    return ok


def main(argv):
    if argv[1:] == ["--self-test"]:
        return 0 if self_test() else 1
    if len(argv) < 2:
        print(__doc__)
        return 2
    bad = False
    for root in argv[1:]:
        files, problems = check_tree(root)
        for p in problems:
            print("PROBLEM", p)
        print(f"{root}: {files} files checked, {len(problems)} problems")
        bad = bad or bool(problems)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
