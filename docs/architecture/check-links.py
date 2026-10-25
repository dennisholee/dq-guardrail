#!/usr/bin/env python3
"""Resolve every internal markdown link and anchor across the architecture docs.

Checks, for each markdown file given on the command line (or discovered under
the architecture folder):
  - relative  [text](file.md)          -> file must exist
  - cross-doc [text](file.md#anchor)   -> file and anchor must exist
  - same-doc  [text](#anchor)          -> anchor must exist

Exits non-zero on the first broken reference, listing all of them.

Usage: python3 check-links.py [file.md ...]
"""
import os
import re
import sys

LINK = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
SKIP_PREFIX = ("http://", "https://", "mailto:")


def slug(heading: str) -> str:
    """GitHub-style anchor slug for a heading."""
    text = "".join(c for c in heading.strip().lower() if c.isalnum() or c in "-_ ")
    return text.replace(" ", "-")


def anchors_of(path: str) -> set:
    found = set()
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            if line.startswith("#"):
                found.add(slug(line.lstrip("#")))
    return found


def check(path: str) -> list:
    problems = []
    base = os.path.dirname(os.path.abspath(path))
    own = anchors_of(path)
    with open(path, encoding="utf-8") as handle:
        for number, line in enumerate(handle, start=1):
            for target in LINK.findall(line):
                if target.startswith(SKIP_PREFIX):
                    continue
                file_part, _, anchor = target.partition("#")
                if not file_part:                       # same-document anchor
                    if anchor not in own:
                        problems.append(f"{path}:{number} missing anchor #{anchor}")
                    continue
                resolved = os.path.normpath(os.path.join(base, file_part))
                if not os.path.exists(resolved):
                    problems.append(f"{path}:{number} missing file {file_part}")
                elif anchor and anchor not in anchors_of(resolved):
                    problems.append(
                        f"{path}:{number} missing anchor #{anchor} in {file_part}"
                    )
    return problems


def main(argv: list) -> int:
    files = argv[1:]
    if not files:
        root = os.path.dirname(os.path.abspath(__file__))
        files = [
            os.path.join(root, name)
            for name in sorted(os.listdir(root))
            if name.endswith(".md")
        ]
    problems = []
    for path in files:
        problems.extend(check(path))
    for problem in problems:
        print(f"broken: {problem}")
    print(f"checked {len(files)} file(s), {len(problems)} broken reference(s)")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
