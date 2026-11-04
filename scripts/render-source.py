#!/usr/bin/env python3
"""Salin source POC lengkap ke appendix dokumentasi tanpa mengubah source."""
from pathlib import Path

root = Path(__file__).resolve().parents[1]
doc = root / "docs/04-prototype.md"
marker = "<!-- GENERATED SOURCE -->"
intro = doc.read_text().split(marker)[0].rstrip()
paths = [Path("go.mod"), Path("Makefile")]
paths += sorted(p.relative_to(root) for p in (root / "cmd").rglob("*.go"))
paths += sorted(p.relative_to(root) for p in (root / "internal").rglob("*.go") if not p.name.endswith("_test.go"))
parts = [intro, "", marker, ""]
for path in paths:
    lang = "go" if path.suffix == ".go" else ("makefile" if path.name == "Makefile" else "text")
    parts += [f"`{path.as_posix()}`", "", f"```{lang}", (root / path).read_text().rstrip(), "```", ""]
doc.write_text("\n".join(parts))
