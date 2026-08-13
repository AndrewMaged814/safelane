from __future__ import annotations

import os
import threading
import uuid
from contextlib import contextmanager
from pathlib import Path


_LOCKS_GUARD = threading.Lock()
_LOCKS: dict[Path, threading.RLock] = {}


@contextmanager
def state_lock(directory: Path):
    key = directory.resolve()
    with _LOCKS_GUARD:
        lock = _LOCKS.setdefault(key, threading.RLock())
    with lock:
        yield


def atomic_write(path: Path, data: bytes) -> None:
    with state_lock(path.parent):
        temporary = path.with_name(
            f".{path.name}.{os.getpid()}.{uuid.uuid4().hex}.tmp"
        )
        try:
            temporary.write_bytes(data)
            os.replace(temporary, path)
        finally:
            if temporary.exists():
                temporary.unlink()
