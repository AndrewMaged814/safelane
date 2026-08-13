from __future__ import annotations

import os
import re
import secrets
import tempfile
import threading
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
    validate_artifact,
)
from .engine import ResolutionError, SafeLaneEngine
from .pr_studio import PullRequestStudioError, PullRequestStudioService
from .repository_studio import RepositoryStudioService


_STATIC = Path(__file__).with_name("studio_static")
_MAX_REQUEST_BYTES = 16_384
_APPROVAL_KEYS = {
    "selected_profile",
    "assessment_id",
    "head_sha",
    "assessment_input_sha256",
    "assessment_result_sha256",
}


class StudioRequestError(ValueError):
    pass


class StudioConflict(RuntimeError):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


class StudioWorkspaceError(RuntimeError):
    pass


class StudioWriteError(RuntimeError):
    pass


class StudioWorkspace:
    def __init__(self, path: Path):
        resolved = path.resolve()
        if not resolved.is_dir():
            raise StudioWorkspaceError(f"Studio workspace is not a directory: {resolved}")
        self.path = resolved
        self.assessment_path = resolved / "assessment.json"
        self.decision_path = resolved / "decision.json"
        self._lock_path = resolved / ".safelane.lock"
        self._thread_lock = threading.RLock()

    @contextmanager
    def exclusive(self) -> Iterator[None]:
        with self._thread_lock:
            with self._lock_path.open("a+b") as lock_file:
                lock_file.seek(0, os.SEEK_END)
                if lock_file.tell() == 0:
                    lock_file.write(b"\0")
                    lock_file.flush()
                    os.fsync(lock_file.fileno())
                lock_file.seek(0)
                if os.name == "nt":
                    import msvcrt

                    msvcrt.locking(lock_file.fileno(), msvcrt.LK_LOCK, 1)
                    try:
                        yield
                    finally:
                        lock_file.seek(0)
                        msvcrt.locking(lock_file.fileno(), msvcrt.LK_UNLCK, 1)
                else:
                    import fcntl

                    fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
                    try:
                        yield
                    finally:
                        fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)

    def _load_unlocked(self) -> tuple[dict[str, Any], dict[str, Any] | None]:
        try:
            assessment = load_json_bytes(self.assessment_path.read_bytes())
            validate_artifact("assessment-v2", assessment)
        except (OSError, ArtifactError) as exc:
            raise StudioWorkspaceError(f"invalid current assessment: {exc}") from exc

        decision: dict[str, Any] | None = None
        if self.decision_path.exists():
            try:
                decision = load_json_bytes(self.decision_path.read_bytes())
                validate_artifact("decision-v3", decision)
            except (OSError, ArtifactError) as exc:
                raise StudioWorkspaceError(f"invalid current decision: {exc}") from exc
        return assessment, decision


class StudioService:
    def __init__(
        self,
        workspace: StudioWorkspace,
        engine: SafeLaneEngine,
        *,
        clock: Callable[[], str] | None = None,
    ) -> None:
        self.workspace = workspace
        self._engine = engine
        self._clock = clock or _utc_now
        self.approval_token = secrets.token_urlsafe(32)

    def current(self) -> dict[str, Any]:
        with self.workspace.exclusive():
            return self._current_unlocked()

    def _current_unlocked(self) -> dict[str, Any]:
        assessment, decision = self.workspace._load_unlocked()
        try:
            self._engine.validate_workspace_artifacts(assessment, decision)
        except (ArtifactError, ResolutionError) as exc:
            raise StudioWorkspaceError(f"inconsistent current artifacts: {exc}") from exc
        return {
            "assessment": assessment,
            "decision_path": (
                str(self.workspace.decision_path) if decision is not None else None
            ),
            "approval_token": self.approval_token,
        }

    def approve(self, payload: Any, *, approval_token: str | None) -> dict[str, Any]:
        if approval_token is None or not secrets.compare_digest(
            approval_token, self.approval_token
        ):
            raise StudioRequestError("invalid approval token")
        if not isinstance(payload, dict) or set(payload) != _APPROVAL_KEYS:
            raise StudioRequestError("invalid approval request")
        if not all(isinstance(payload[key], str) for key in _APPROVAL_KEYS):
            raise StudioRequestError("approval fields must be strings")

        with self.workspace.exclusive():
            snapshot = self._current_unlocked()
            current = snapshot["assessment"]
            if current["review"]["status"] != "unresolved" or snapshot["decision_path"] is not None:
                raise StudioConflict("approval_conflict")
            expected = {
                "assessment_id": current["assessment_id"],
                "head_sha": current["change"]["head_sha"],
                "assessment_input_sha256": current["assessment_input_sha256"],
                "assessment_result_sha256": current["assessment_result_sha256"],
            }
            if any(payload[key] != value for key, value in expected.items()):
                raise StudioConflict("stale_approval")
            event = {
                "type": "human",
                "selected_profile": payload["selected_profile"],
                "resolved_at": self._clock(),
                **expected,
            }
            try:
                resolved = self._engine.approve(current, event)
            except (ArtifactError, ResolutionError) as exc:
                raise StudioConflict("approval_conflict") from exc
            try:
                _atomic_write(
                    self.workspace.assessment_path,
                    canonical_json_bytes(resolved.assessment),
                )
                _atomic_write(
                    self.workspace.decision_path,
                    canonical_json_bytes(resolved.decision),
                )
            except OSError as exc:
                raise StudioWriteError("failed to publish approval artifacts") from exc
            return self._current_unlocked()


def create_studio_server(
    service: StudioService | PullRequestStudioService | RepositoryStudioService,
    *,
    port: int = 4173,
) -> ThreadingHTTPServer:
    class Handler(_StudioHandler):
        studio_service = service

    return ThreadingHTTPServer(("127.0.0.1", port), Handler)


def serve_studio(
    service: StudioService | PullRequestStudioService | RepositoryStudioService, *, port: int = 4173
) -> None:
    server = create_studio_server(service, port=port)
    try:
        print(f"SafeLane Studio: http://127.0.0.1:{server.server_port}")
        if isinstance(service, (PullRequestStudioService, RepositoryStudioService)):
            print(f"Repository: {service.provider.repository}")
            print(f"Workspace: {service.workspace}")
        else:
            print(f"Workspace: {service.workspace.path}")
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


class _StudioHandler(BaseHTTPRequestHandler):
    studio_service: StudioService | PullRequestStudioService | RepositoryStudioService

    def do_GET(self) -> None:
        if not self._trusted_host():
            self._json(403, {"error": "untrusted_request"})
            return
        path = urlsplit(self.path).path
        if isinstance(self.studio_service, (PullRequestStudioService, RepositoryStudioService)):
            if path == "/api/dashboard":
                try:
                    result = self.studio_service.dashboard()
                    result["approval_token"] = self.studio_service.approval_token
                    self._json(200, result)
                except PullRequestStudioError as exc:
                    if isinstance(self.studio_service, RepositoryStudioService):
                        self._json(422, {
                            "error": "repository_assessment_failed",
                            "message": str(exc),
                        })
                    else:
                        self._json(502, {"error": "repository_unavailable"})
                return
            if path == "/api/profiles":
                result = self.studio_service.profiles()
                result["repository"] = self.studio_service.provider.repository
                result["approval_token"] = self.studio_service.approval_token
                self._json(200, result)
                return
            if path == "/api/outcomes" and isinstance(
                self.studio_service, RepositoryStudioService
            ):
                result = self.studio_service.outcomes()
                result["repository"] = self.studio_service.provider.repository
                self._json(200, result)
                return
            assessment_match = re.fullmatch(r"/api/assessments/(\d+)", path)
            if assessment_match:
                try:
                    assessment = self.studio_service.assessment(
                        int(assessment_match.group(1))
                    )
                    self._json(200, {
                        "assessment": assessment,
                        "approval_token": self.studio_service.approval_token,
                        "github_check": (
                            self.studio_service.check_projection(
                                int(assessment_match.group(1))
                            )
                            if isinstance(
                                self.studio_service, RepositoryStudioService
                            )
                            else None
                        ),
                    })
                except PullRequestStudioError:
                    self._json(404, {"error": "pull_request_not_found"})
                return
        elif path == "/api/assessment":
            try:
                self._json(200, self.studio_service.current())
            except StudioWorkspaceError:
                self._json(500, {"error": "workspace_invalid"})
            return
        static = {
            "/": (_STATIC / "index.html", "text/html; charset=utf-8"),
            "/styles.css": (_STATIC / "styles.css", "text/css; charset=utf-8"),
            "/app.js": (_STATIC / "app.js", "text/javascript; charset=utf-8"),
            "/safelane-logo.svg": (
                Path(__file__).resolve().parents[2] / "assets/brand/safelane-logo.svg",
                "image/svg+xml",
            ),
            "/safelane-mark.svg": (
                Path(__file__).resolve().parents[2] / "assets/brand/safelane-mark.svg",
                "image/svg+xml",
            ),
        }.get(path)
        if static is None and (
            path in {"/changes", "/profiles", "/outcomes"}
            or re.fullmatch(r"/changes/\d+", path)
        ):
            static = (_STATIC / "index.html", "text/html; charset=utf-8")
        if static is None:
            self._json(404, {"error": "not_found"})
            return
        asset_path, content_type = static
        try:
            raw = asset_path.read_bytes()
        except OSError:
            self._json(500, {"error": "static_asset_unavailable"})
            return
        self._send(200, raw, content_type)

    def do_POST(self) -> None:
        path = urlsplit(self.path).path
        pr_approval = re.fullmatch(r"/api/assessments/(\d+)/approve", path)
        pr_resolution = re.fullmatch(r"/api/assessments/(\d+)/resolve", path)
        pr_compilation = re.fullmatch(r"/api/assessments/(\d+)/compile", path)
        pr_outcome = re.fullmatch(r"/api/assessments/(\d+)/outcomes", path)
        repository_connection = path == "/api/connect"
        if isinstance(self.studio_service, (PullRequestStudioService, RepositoryStudioService)):
            if (
                pr_approval is None
                and pr_resolution is None
                and pr_compilation is None
                and pr_outcome is None
                and not repository_connection
            ):
                self._json(404, {"error": "not_found"})
                return
        elif path != "/api/approve":
            self._json(404, {"error": "not_found"})
            return
        if not self._trusted_browser_request():
            self._json(403, {"error": "untrusted_request"})
            return
        if self.headers.get_content_type() != "application/json":
            self._json(400, {"error": "invalid_request"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self._json(400, {"error": "invalid_request"})
            return
        if length <= 0 or length > _MAX_REQUEST_BYTES:
            self._json(400, {"error": "invalid_request"})
            return
        try:
            payload = load_json_bytes(self.rfile.read(length))
            if isinstance(self.studio_service, (PullRequestStudioService, RepositoryStudioService)):
                if repository_connection:
                    result = self.studio_service.connect(
                        payload,
                        approval_token=self.headers.get("X-SafeLane-CSRF"),
                    )
                    result["approval_token"] = self.studio_service.approval_token
                else:
                    if pr_outcome is not None and isinstance(
                        self.studio_service, RepositoryStudioService
                    ):
                        result = self.studio_service.record_outcome(
                            int(pr_outcome.group(1)), payload,
                            approval_token=self.headers.get("X-SafeLane-CSRF"),
                        )
                    elif pr_compilation is not None and isinstance(
                        self.studio_service, RepositoryStudioService
                    ):
                        result = self.studio_service.compile(
                            int(pr_compilation.group(1)), payload,
                            approval_token=self.headers.get("X-SafeLane-CSRF"),
                        )
                    elif pr_resolution is not None and isinstance(
                        self.studio_service, RepositoryStudioService
                    ):
                        result = self.studio_service.resolve(
                            int(pr_resolution.group(1)), payload,
                            approval_token=self.headers.get("X-SafeLane-CSRF"),
                        )
                    else:
                        assert pr_approval is not None
                        result = self.studio_service.approve(
                            int(pr_approval.group(1)),
                            payload,
                            approval_token=self.headers.get("X-SafeLane-CSRF"),
                        )
            else:
                result = self.studio_service.approve(
                    payload,
                    approval_token=self.headers.get("X-SafeLane-CSRF"),
                )
        except PullRequestStudioError as exc:
            if repository_connection:
                self._json(400, {
                    "error": "repository_connection_failed",
                    "message": str(exc),
                })
            else:
                self._json(400, {"error": "invalid_request"})
        except (ArtifactError, StudioRequestError):
            self._json(400, {"error": "invalid_request"})
        except StudioConflict as exc:
            self._json(409, {"error": exc.code})
        except StudioWorkspaceError:
            self._json(500, {"error": "workspace_invalid"})
        except StudioWriteError:
            self._json(500, {"error": "artifact_write_failed"})
        else:
            self._json(200, result)

    def _trusted_host(self) -> bool:
        host = self.headers.get("Host", "").lower()
        port = self.server.server_port
        suffix = "" if port == 80 else f":{port}"
        return host in {f"127.0.0.1{suffix}", f"localhost{suffix}"}

    def _trusted_browser_request(self) -> bool:
        return True

    def _json(self, status: int, value: Any) -> None:
        self._send(status, canonical_json_bytes(value), "application/json; charset=utf-8")

    def _send(self, status: int, raw: bytes, content_type: str) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; "
            "connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
        )
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, format: str, *args: object) -> None:
        del format, args


def _atomic_write(path: Path, raw: bytes) -> None:
    temporary_name: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="wb",
            prefix=f".{path.name}.",
            suffix=".tmp",
            dir=path.parent,
            delete=False,
        ) as temporary:
            temporary_name = temporary.name
            temporary.write(raw)
            temporary.flush()
            os.fsync(temporary.fileno())
        _durable_replace(Path(temporary_name), path)
        temporary_name = None
    finally:
        if temporary_name is not None:
            try:
                Path(temporary_name).unlink()
            except FileNotFoundError:
                pass


def _durable_replace(source: Path, destination: Path) -> None:
    if os.name == "nt":
        import ctypes
        from ctypes import wintypes

        move_file = ctypes.WinDLL("kernel32", use_last_error=True).MoveFileExW
        move_file.argtypes = [wintypes.LPCWSTR, wintypes.LPCWSTR, wintypes.DWORD]
        move_file.restype = wintypes.BOOL
        replace_existing = 0x1
        write_through = 0x8
        if not move_file(str(source), str(destination), replace_existing | write_through):
            raise ctypes.WinError(ctypes.get_last_error())
        return

    os.replace(source, destination)
    directory_fd = os.open(destination.parent, os.O_RDONLY)
    try:
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
