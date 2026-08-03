from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
MARKDOWN_LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
REQUIRED_DOCS = (
    ROOT / "README.md",
    ROOT / "SECURITY.md",
    ROOT / "docs" / "security" / "threat-model.md",
    ROOT / "docs" / "security" / "secure-configuration.md",
)


def local_target(source: Path, raw_target: str) -> Path | None:
    target = raw_target.strip().split(maxsplit=1)[0].strip("<>")
    if not target or target.startswith(("#", "http://", "https://", "mailto:")):
        return None
    path_text = unquote(target.split("#", 1)[0])
    return (source.parent / path_text).resolve()


def main() -> int:
    errors: list[str] = []
    for required in REQUIRED_DOCS:
        if not required.is_file():
            errors.append(f"missing required documentation: {required.relative_to(ROOT)}")

    sources = [ROOT / "README.md", ROOT / "SECURITY.md", *sorted((ROOT / "docs").rglob("*.md"))]
    for source in sources:
        content = source.read_text(encoding="utf-8")
        for line_number, line in enumerate(content.splitlines(), start=1):
            for match in MARKDOWN_LINK.finditer(line):
                target = local_target(source, match.group(1))
                if target is not None and not target.exists():
                    errors.append(
                        f"{source.relative_to(ROOT)}:{line_number}: broken local link {match.group(1)!r}"
                    )

    if errors:
        print("Documentation validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"Documentation validation passed for {len(sources)} Markdown files.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
