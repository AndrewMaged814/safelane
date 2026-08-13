from __future__ import annotations

from safelane.change_classes import (
    GITSTREAM_SENSITIVE_PATH_PREFIXES,
    classify_change,
)
from safelane.diff_evidence import parse_diff


def _class(files: list[str], diff: str = "", *, sensitive=None):
    spans, parsed, _ = parse_diff(diff) if diff else ((), tuple(files), 0)
    return classify_change(
        files or parsed,
        spans,
        sensitive_path_prefixes=(
            GITSTREAM_SENSITIVE_PATH_PREFIXES if sensitive is None else sensitive
        ),
    )


def test_empty_file_list_is_not_all_docs() -> None:
    # gitStream `every` on an empty list is false.
    result = _class([])

    assert result.all_docs is False
    assert result.all_tests is False
    assert result.all_images is False
    assert result.formatting_only is False
    assert result.safe_change is False


def test_markdown_and_rst_are_docs_but_requirements_txt_is_not() -> None:
    docs = _class(["README.md", "notes.mkdown", "guide.txt", "api.rst", "book.adoc"])
    requirements = _class(["requirements.txt"])
    mixed = _class(["README.md", "requirements.txt"])

    assert docs.all_docs is True
    assert requirements.all_docs is False
    assert mixed.all_docs is False
    assert docs.safe_change is True


def test_images_are_only_svg_png_gif() -> None:
    images = _class(["logo.svg", "shot.png", "anim.gif"])
    jpeg = _class(["photo.jpg"])

    assert images.all_images is True
    assert images.safe_change is True
    assert jpeg.all_images is False
    assert jpeg.safe_change is False


def test_docs_mixed_with_images_is_not_a_safe_change() -> None:
    # gitStream ORs the all-* predicates; a mix fails every one of them.
    result = _class(["README.md", "logo.png"])

    assert result.all_docs is False
    assert result.all_images is False
    assert result.safe_change is False


def test_test_token_matches_padded_paths_and_rejects_testing_or_contest() -> None:
    # Regex: [^a-zA-Z0-9](spec|test|tests)[^a-zA-Z0-9], path padded as /{path}/.
    tests = _class(["test.py", "src/foo_spec.rb", "app/tests/api.py"])
    not_tests = _class(["testing/helper.py"])
    contest = _class(["contest.py"])

    assert tests.all_tests is True
    assert tests.safe_change is True
    assert not_tests.all_tests is False
    assert contest.all_tests is False


def test_python_indent_only_diff_is_formatting() -> None:
    diff = """diff --git a/src/app.py b/src/app.py
--- a/src/app.py
+++ b/src/app.py
@@ -1,2 +1,2 @@
-def foo():
-    return 1
+def foo():
+  return 1
"""
    result = _class(["src/app.py"], diff)

    assert result.formatting_only is True
    assert result.safe_change is True


def test_python_behavior_change_is_not_formatting() -> None:
    diff = """diff --git a/src/app.py b/src/app.py
--- a/src/app.py
+++ b/src/app.py
@@ -1 +1 @@
-return 1
+return 2
"""
    result = _class(["src/app.py"], diff)

    assert result.formatting_only is False
    assert result.safe_change is False


def test_json_key_reorder_and_whitespace_is_formatting() -> None:
    diff = """diff --git a/config.json b/config.json
--- a/config.json
+++ b/config.json
@@ -1 +1 @@
-{"b": 1, "a": 2}
+{"a":2,"b":1}
"""
    result = _class(["config.json"], diff)

    assert result.formatting_only is True
    assert result.safe_change is True


def test_unsupported_java_whitespace_falls_back_to_normalization() -> None:
    diff = """diff --git a/Main.java b/Main.java
--- a/Main.java
+++ b/Main.java
@@ -1 +1 @@
-int x = 1;
+int x=1;
"""
    result = _class(["Main.java"], diff)

    assert result.formatting_only is True
    assert result.safe_change is True


def test_sensitive_paths_use_substring_match_on_the_org_list() -> None:
    # gitStream: files | match(list=sensitive_files) | some
    hit = _class(["src/app/auth/login.py"])
    miss = _class(["src/demo_api/auth/login.py"])
    custom = _class(
        ["src/demo_api/auth/login.py"],
        sensitive=("src/demo_api/auth/",),
    )

    assert hit.sensitive_paths is True
    assert miss.sensitive_paths is False
    assert custom.sensitive_paths is True
    assert GITSTREAM_SENSITIVE_PATH_PREFIXES == (
        "src/app/auth/",
        "src/app/routing/",
        "src/app/resources/",
    )


def test_docs_inside_a_sensitive_directory_are_still_marked_sensitive() -> None:
    result = _class(["src/app/auth/README.md"])

    assert result.all_docs is True
    assert result.safe_change is True
    assert result.sensitive_paths is True
