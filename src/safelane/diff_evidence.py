from __future__ import annotations

import re
from dataclasses import dataclass


@dataclass(frozen=True)
class DiffSpan:
    file: str
    side: str
    line: int
    text: str


_HUNK = re.compile(r"^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@")


def parse_diff(diff: str) -> tuple[tuple[DiffSpan, ...], tuple[str, ...], int]:
    current_file: str | None = None
    old_line = new_line = 0
    spans: list[DiffSpan] = []
    files: list[str] = []
    lines_changed = 0
    in_hunk = False
    for line in diff.splitlines():
        if line.startswith("diff --git a/"):
            match = re.match(r"^diff --git a/(.+) b/(.+)$", line)
            in_hunk = False
            current_file = None
            if match and match.group(1) == match.group(2):
                current_file = match.group(2)
                if current_file not in files:
                    files.append(current_file)
            continue
        if line.startswith("+++ b/"):
            current_file = line[6:]
            if current_file not in files:
                files.append(current_file)
            continue
        match = _HUNK.match(line)
        if match:
            old_line, new_line = map(int, match.groups())
            in_hunk = True
            continue
        if not in_hunk or current_file is None or line.startswith("\\ No newline"):
            continue
        prefix, text = line[:1], line[1:]
        if prefix == " ":
            old_line += 1
            new_line += 1
        elif prefix == "-":
            spans.append(DiffSpan(current_file, "removed", old_line, text))
            old_line += 1
            lines_changed += 1
        elif prefix == "+":
            spans.append(DiffSpan(current_file, "added", new_line, text))
            new_line += 1
            lines_changed += 1
    return tuple(spans), tuple(files), lines_changed


def parse_diff_metadata(raw: bytes) -> tuple[tuple[str, ...], bool]:
    files: list[str] = []
    binary_patch = False
    for line in raw.splitlines():
        if line == b"GIT binary patch" or (
            line.startswith(b"Binary files ") and line.endswith(b" differ")
        ):
            binary_patch = True
        if not line.startswith(b"diff --git "):
            continue
        match = re.match(rb"^diff --git a/(.+) b/(.+)$", line)
        if match and match.group(1) == match.group(2):
            try:
                path = match.group(2).decode("utf-8")
            except UnicodeDecodeError:
                path = f"<unrecognized-path-{len(files)}>"
        else:
            path = f"<unrecognized-path-{len(files)}>"
        files.append(path)
    return tuple(files), binary_patch
