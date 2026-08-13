from __future__ import annotations

import re
import secrets
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
)
from .pr_studio import PullRequestStudioError, PullRequestStudioService
from .repository_studio import RepositoryStudioService


_STATIC = Path(__file__).with_name("studio_static")
_MAX_REQUEST_BYTES = 16_384

StudioService = PullRequestStudioService | RepositoryStudioService


class StudioRequestError(ValueError):
    pass


class StudioConflict(RuntimeError):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


def create_studio_server(
    service: StudioService,
    *,
    port: int = 4173,
) -> ThreadingHTTPServer:
    class Handler(_StudioHandler):
        studio_service = service

    return ThreadingHTTPServer(("127.0.0.1", port), Handler)


def serve_studio(service: StudioService, *, port: int = 4173) -> None:
    server = create_studio_server(service, port=port)
    try:
        print(f"SafeLane Studio: http://127.0.0.1:{server.server_port}")
        print(f"Repository: {service.provider.repository}")
        print(f"Workspace: {service.workspace}")
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


class _StudioHandler(BaseHTTPRequestHandler):
    studio_service: StudioService

    def do_GET(self) -> None:
        if not self._trusted_host():
            self._json(403, {"error": "untrusted_request"})
            return
        path = urlsplit(self.path).path
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
                        if isinstance(self.studio_service, RepositoryStudioService)
                        else None
                    ),
                })
            except PullRequestStudioError:
                self._json(404, {"error": "pull_request_not_found"})
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
        if (
            pr_approval is None
            and pr_resolution is None
            and pr_compilation is None
            and pr_outcome is None
            and not repository_connection
        ):
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
            if repository_connection:
                result = self.studio_service.connect(
                    payload,
                    approval_token=self.headers.get("X-SafeLane-CSRF"),
                )
                result["approval_token"] = self.studio_service.approval_token
            elif pr_outcome is not None and isinstance(
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
        else:
            self._json(200, result)

    def _trusted_host(self) -> bool:
        host = self.headers.get("Host", "").lower()
        port = self.server.server_port
        suffix = "" if port == 80 else f":{port}"
        return host in {f"127.0.0.1{suffix}", f"localhost{suffix}"}

    def _trusted_browser_request(self) -> bool:
        if not self._trusted_host():
            return False
        host = self.headers.get("Host", "").lower()
        origin = self.headers.get("Origin", "").lower()
        token = self.headers.get("X-SafeLane-CSRF")
        return (
            origin == f"http://{host}"
            and token is not None
            and secrets.compare_digest(token, self.studio_service.approval_token)
        )

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
