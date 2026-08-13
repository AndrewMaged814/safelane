from __future__ import annotations

import os
import threading
import uuid
from contextlib import contextmanager
from pathlib import Path
from typing import BinaryIO


_LOCKS_GUARD = threading.Lock()
_LOCKS: dict[Path, threading.RLock] = {}
_LOCAL = threading.local()


@contextmanager
def state_lock(directory: Path):
    key = directory.resolve()
    with _LOCKS_GUARD:
        lock = _LOCKS.setdefault(key, threading.RLock())
    with lock:
        depths = getattr(_LOCAL, "depths", {})
        depth = depths.get(key, 0)
        handle: BinaryIO | None = None
        if depth == 0:
            key.mkdir(parents=True, exist_ok=True)
            handle = (key / ".safelane.lock").open("a+b")
            _lock_file(handle)
        depths[key] = depth + 1
        _LOCAL.depths = depths
        try:
            yield
        finally:
            depths[key] -= 1
            if depths[key] == 0:
                del depths[key]
                assert handle is not None
                _unlock_file(handle)
                handle.close()


def _lock_file(handle: BinaryIO) -> None:
    handle.seek(0)
    if os.name == "nt":
        import msvcrt

        if handle.read(1) == b"":
            handle.seek(0)
            handle.write(b"0")
            handle.flush()
        handle.seek(0)
        msvcrt.locking(handle.fileno(), msvcrt.LK_LOCK, 1)
    else:
        import fcntl

        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)


def _unlock_file(handle: BinaryIO) -> None:
    handle.seek(0)
    if os.name == "nt":
        import msvcrt

        msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
    else:
        import fcntl

        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


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
