from __future__ import annotations

import hashlib
import hmac
import os
import secrets
from pathlib import Path
from typing import Any

from .artifacts import canonical_json_bytes
from .state_io import atomic_write, state_lock


_PROCESS_KEY = secrets.token_bytes(32)


def _process_authorization_key() -> bytes:
    return _PROCESS_KEY


def authorization_signature(value: Any, key: bytes) -> str:
    if len(key) < 32:
        raise ValueError("authorization key must contain at least 32 bytes")
    digest = hmac.new(key, canonical_json_bytes(value), hashlib.sha256).hexdigest()
    return f"hmac-sha256:{digest}"


def signature_matches(value: Any, signature: str, key: bytes) -> bool:
    try:
        expected = authorization_signature(value, key)
    except (TypeError, ValueError):
        return False
    return hmac.compare_digest(expected, signature)


def _load_or_create_authorization_key(repository: str) -> bytes:
    root = Path(
        os.environ.get("LOCALAPPDATA")
        or os.environ.get("XDG_DATA_HOME")
        or (Path.home() / ".local" / "share")
    )
    identifier = hashlib.sha256(repository.encode("utf-8")).hexdigest()
    path = root / "SafeLane" / "authorization-keys" / f"{identifier}.key"
    with state_lock(path.parent):
        try:
            key = path.read_bytes()
        except FileNotFoundError:
            key = secrets.token_bytes(32)
            atomic_write(path, key)
            try:
                path.chmod(0o600)
            except OSError:
                pass
    if len(key) != 32:
        raise ValueError(f"invalid SafeLane authorization key: {path}")
    return key
