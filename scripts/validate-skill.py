#!/usr/bin/env python3
from __future__ import annotations

import pathlib
import re
import sys


def fail(message: str) -> None:
    print(f"skill validation failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def main() -> None:
    if len(sys.argv) != 2:
        fail("usage: validate-skill.py <skill-directory>")
    root = pathlib.Path(sys.argv[1]).resolve()
    skill = root / "SKILL.md"
    if not skill.is_file():
        fail("SKILL.md is missing")
    text = skill.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        fail("frontmatter is missing")
    parts = text.split("---\n", 2)
    if len(parts) != 3:
        fail("frontmatter is not closed")
    frontmatter = parts[1]
    name = re.search(r"^name:\s*([a-z0-9-]+)\s*$", frontmatter, re.MULTILINE)
    description = re.search(r"^description:\s*(.+)$", frontmatter, re.MULTILINE)
    if not name or name.group(1) != root.name:
        fail("name must match the skill directory")
    if not description or len(description.group(1).strip()) < 40:
        fail("description is missing or not discriminating")
    if "TODO" in text or "PLACEHOLDER" in text:
        fail("unfinished scaffold text remains")
    for target in re.findall(r"\]\((references/[^)]+)\)", text):
        if not (root / target).is_file():
            fail(f"referenced file is missing: {target}")
    openai = root / "agents" / "openai.yaml"
    if not openai.is_file() or "$sam-harness" not in openai.read_text(encoding="utf-8"):
        fail("agents/openai.yaml must contain a default prompt using $sam-harness")
    print(f"skill validation passed: {root}")


if __name__ == "__main__":
    main()
