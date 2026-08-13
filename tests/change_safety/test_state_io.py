from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from safelane.state_io import atomic_write, state_lock
from safelane.authorization import load_or_create_authorization_key


def test_same_process_writers_use_unique_temporary_files(tmp_path: Path) -> None:
    target = tmp_path / "state.json"
    values = [f'{{"writer":{index}}}\n'.encode() for index in range(20)]

    with ThreadPoolExecutor(max_workers=8) as pool:
        list(pool.map(lambda value: atomic_write(target, value), values))

    assert target.read_bytes() in values
    assert not list(tmp_path.glob("*.tmp"))


def test_state_lock_serializes_one_repository_pr_directory(tmp_path: Path) -> None:
    directory = tmp_path / "acme--payments" / "pr-42"
    order: list[int] = []

    def append(index: int) -> None:
        with state_lock(directory):
            order.append(index)

    with ThreadPoolExecutor(max_workers=8) as pool:
        list(pool.map(append, range(20)))

    assert sorted(order) == list(range(20))


def test_repository_authorization_key_is_stable_across_loads(
    tmp_path: Path, monkeypatch
) -> None:
    monkeypatch.setenv("LOCALAPPDATA", str(tmp_path))

    first = load_or_create_authorization_key("acme/payments")
    second = load_or_create_authorization_key("acme/payments")

    assert first == second
    assert len(first) == 32
