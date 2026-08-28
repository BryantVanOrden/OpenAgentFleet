"""OS Workspace Snapshot and Rollback Time-Machine Engine.

Captures copy-on-write workspace snapshots before risky operations (file writes, pip installs,
build scripts) and allows instantaneous rollback if assertions or tasks fail.
"""

from __future__ import annotations

import os
import shutil
import tarfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Optional


@dataclass
class SnapshotMetadata:
    id: str
    name: str
    created_at: float
    file_count: int
    size_bytes: int
    archive_path: str


class SnapshotEngine:
    """Manages workspace snapshots and rollbacks in the agent container."""

    def __init__(self, workspace_root: str = "/home/agent/workspace", snapshot_store: str = "/tmp/agentfleet_snapshots") -> None:
        self.workspace_root = Path(workspace_root)
        self.snapshot_store = Path(snapshot_store)
        self.snapshot_store.mkdir(parents=True, exist_ok=True)
        self.history: list[SnapshotMetadata] = []

    def create_snapshot(self, name: str) -> SnapshotMetadata:
        """Create a tar.gz snapshot of the current workspace directory."""
        snap_id = f"snap-{int(time.time() * 1000)}"
        archive_file = self.snapshot_store / f"{snap_id}.tar.gz"

        file_count = 0
        total_size = 0

        if not self.workspace_root.exists():
            self.workspace_root.mkdir(parents=True, exist_ok=True)

        with tarfile.open(archive_file, "w:gz") as tar:
            for root, _, files in os.walk(self.workspace_root):
                for f in files:
                    fp = Path(root) / f
                    if not fp.is_symlink() and fp.exists():
                        file_count += 1
                        total_size += fp.stat().st_size
                        rel_path = fp.relative_to(self.workspace_root)
                        tar.add(fp, arcname=str(rel_path))

        meta = SnapshotMetadata(
            id=snap_id,
            name=name,
            created_at=time.time(),
            file_count=file_count,
            size_bytes=archive_file.stat().st_size if archive_file.exists() else 0,
            archive_path=str(archive_file),
        )
        self.history.append(meta)
        return meta

    def rollback(self, snapshot_id: Optional[str] = None) -> bool:
        """Rollback workspace to the specified snapshot ID or the most recent snapshot."""
        if not self.history:
            return False

        target: Optional[SnapshotMetadata] = None
        if snapshot_id:
            for s in self.history:
                if s.id == snapshot_id:
                    target = s
                    break
        else:
            target = self.history[-1]

        if not target or not Path(target.archive_path).exists():
            return False

        # Clear current workspace contents cleanly
        if self.workspace_root.exists():
            for item in self.workspace_root.iterdir():
                if item.is_dir():
                    shutil.rmtree(item)
                else:
                    item.unlink()
        else:
            self.workspace_root.mkdir(parents=True, exist_ok=True)

        # Restore from tar archive
        with tarfile.open(target.archive_path, "r:gz") as tar:
            try:
                tar.extractall(path=self.workspace_root, filter="data")
            except TypeError:
                tar.extractall(path=self.workspace_root)

        return True

    def list_snapshots(self) -> list[dict]:
        return [
            {
                "id": s.id,
                "name": s.name,
                "created_at": s.created_at,
                "file_count": s.file_count,
                "size_bytes": s.size_bytes,
            }
            for s in self.history
        ]


SNAPSHOT_ENGINE = SnapshotEngine()
