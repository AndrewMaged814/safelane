"""Deterministic change classes matching gitStream's published filters.

Safe-change is the same OR as gitStream's `approve-safe-changes` automation:

    (files | allDocs) or (files | allTests) or (files | allImages)
    or (source.diff.files | isFormattingChange)

Sensitive paths use gitStream's `files | match(list=sensitive_files) | some`
substring match against an org-supplied list. The default list is the example
from gitStream's review-sensitive-files automation, not a SafeLane invention.
"""

from __future__ import annotations

import json
import re
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence

from .diff_evidence import DiffSpan

# gitStream allDocs: md, mkdown, txt, rst, adoc — except requirements.txt.
_DOC_EXTENSIONS = frozenset({".md", ".mkdown", ".txt", ".rst", ".adoc"})
# gitStream allImages: svg, png, gif only.
_IMAGE_EXTENSIONS = frozenset({".svg", ".png", ".gif"})
# gitStream allTests; path is padded as /{path}/ so a leading "test.py" matches.
_TEST_TOKEN = re.compile(r"[^a-zA-Z0-9](spec|test|tests)[^a-zA-Z0-9]")
_WHITESPACE = re.compile(r"\s+")

# https://docs.gitstream.cm/automations/standard/review-assignment/review-sensitive-files/
GITSTREAM_SENSITIVE_PATH_PREFIXES = (
    "src/app/auth/",
    "src/app/routing/",
    "src/app/resources/",
)


@dataclass(frozen=True)
class ChangeClassification:
    all_docs: bool
    all_tests: bool
    all_images: bool
    formatting_only: bool
    safe_change: bool
    sensitive_paths: bool

    @property
    def policy_rule_ids(self) -> tuple[str, ...]:
        rules: list[str] = []
        if self.all_docs:
            rules.append("change_class.docs")
        if self.all_tests:
            rules.append("change_class.tests")
        if self.all_images:
            rules.append("change_class.images")
        if self.formatting_only:
            rules.append("change_class.formatting")
        if self.safe_change:
            rules.append("change_class.safe_change")
        if self.sensitive_paths:
            rules.append("change_class.sensitive")
        return tuple(rules)


def classify_change(
    files: Sequence[str],
    spans: Sequence[DiffSpan] = (),
    *,
    sensitive_path_prefixes: Sequence[str] = GITSTREAM_SENSITIVE_PATH_PREFIXES,
) -> ChangeClassification:
    all_docs = _every(files, is_docs)
    all_tests = _every(files, is_test)
    all_images = _every(files, is_image)
    formatting_only = is_formatting_change(files, spans)
    return ChangeClassification(
        all_docs=all_docs,
        all_tests=all_tests,
        all_images=all_images,
        formatting_only=formatting_only,
        safe_change=all_docs or all_tests or all_images or formatting_only,
        sensitive_paths=has_sensitive_paths(files, sensitive_path_prefixes),
    )


def is_docs(path: str) -> bool:
    normalized = _normalize_path(path)
    if Path(normalized).name.lower() == "requirements.txt":
        return False
    return Path(normalized).suffix.lower() in _DOC_EXTENSIONS


def is_image(path: str) -> bool:
    return Path(_normalize_path(path)).suffix.lower() in _IMAGE_EXTENSIONS


def is_test(path: str) -> bool:
    padded = f"/{_normalize_path(path)}/"
    return _TEST_TOKEN.search(padded) is not None


def is_formatting_change(files: Sequence[str], spans: Sequence[DiffSpan]) -> bool:
    if not files:
        return False
    hunks = _hunks_by_file(spans)
    return all(
        _file_is_formatting(path, *hunks.get(_normalize_path(path), ([], [])))
        for path in files
    )


def has_sensitive_paths(
    files: Sequence[str], prefixes: Sequence[str]
) -> bool:
    if not files or not prefixes:
        return False
    tokens = tuple(_normalize_path(token) for token in prefixes if token)
    return any(
        any(token in _normalize_path(path) for token in tokens) for path in files
    )


def sensitive_path_prefixes_from_policy(policy: dict) -> tuple[str, ...]:
    block = policy.get("change_classes")
    if isinstance(block, dict) and "sensitive_path_prefixes" in block:
        return tuple(block["sensitive_path_prefixes"])
    return GITSTREAM_SENSITIVE_PATH_PREFIXES


def _every(files: Sequence[str], predicate) -> bool:
    # gitStream `every`: empty list is false.
    return bool(files) and all(predicate(path) for path in files)


def _normalize_path(path: str) -> str:
    return path.replace("\\", "/")


def _hunks_by_file(
    spans: Sequence[DiffSpan],
) -> dict[str, tuple[list[str], list[str]]]:
    removed: dict[str, list[str]] = defaultdict(list)
    added: dict[str, list[str]] = defaultdict(list)
    for span in spans:
        key = _normalize_path(span.file)
        if span.side == "removed":
            removed[key].append(span.text)
        elif span.side == "added":
            added[key].append(span.text)
    return {
        path: (removed[path], added[path])
        for path in set(removed) | set(added)
    }


def _file_is_formatting(
    path: str, removed: Iterable[str], added: Iterable[str]
) -> bool:
    old_lines = list(removed)
    new_lines = list(added)
    if not old_lines and not new_lines:
        # No hunk text: cannot prove minify-equality the way gitStream does
        # from full old/new files.
        return False
    old = "\n".join(old_lines)
    new = "\n".join(new_lines)
    return _minified(old, path) == _minified(new, path)


def _minified(text: str, path: str) -> str:
    suffix = Path(_normalize_path(path)).suffix.lower()
    if suffix == ".json":
        compact = _json_minified(text)
        if compact is not None:
            return compact
    return _WHITESPACE.sub("", text)


def _json_minified(text: str) -> str | None:
    try:
        return json.dumps(json.loads(text), separators=(",", ":"), sort_keys=True)
    except (json.JSONDecodeError, TypeError, ValueError):
        return None
