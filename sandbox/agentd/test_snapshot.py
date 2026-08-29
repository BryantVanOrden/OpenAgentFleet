import inspect
import tempfile
import unittest
from pathlib import Path
from snapshot import SnapshotEngine

class TestSnapshotEngine(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.ws_dir = Path(self.temp_dir.name) / "workspace"
        self.snap_dir = Path(self.temp_dir.name) / "snaps"
        self.ws_dir.mkdir(parents=True, exist_ok=True)
        self.engine = SnapshotEngine(workspace_root=str(self.ws_dir), snapshot_store=str(self.snap_dir))

    def tearDown(self):
        self.temp_dir.cleanup()

    def test_create_and_rollback_snapshot(self):
        # 1. Write initial file
        test_file = self.ws_dir / "main.py"
        test_file.write_text("print('version 1')")

        # 2. Snapshot
        snap1 = self.engine.create_snapshot("Initial commit")
        self.assertEqual(snap1.file_count, 1)
        self.assertEqual(len(self.engine.list_snapshots()), 1)

        # 3. Corrupt or modify workspace
        test_file.write_text("corrupted code!")
        bad_file = self.ws_dir / "bad.txt"
        bad_file.write_text("malicious content")
        self.assertTrue(bad_file.exists())

        # 4. Rollback to snap1
        ok = self.engine.rollback(snap1.id)
        self.assertTrue(ok)
        self.assertEqual(test_file.read_text(), "print('version 1')")
        self.assertFalse(bad_file.exists())

    def test_snapshots_default_to_the_directory_the_agent_actually_works_in(self):
        """The default root was /home/agent/workspace, which nothing in the
        image creates. Every snapshot archived an empty directory it had just
        made, and reported success."""
        default = inspect.signature(SnapshotEngine.__init__).parameters["workspace_root"].default
        self.assertEqual(default, "/home/agent/work")

    def test_an_unreadable_archive_leaves_the_workspace_untouched(self):
        """Rollback used to empty the workspace before opening the archive, so
        a truncated snapshot destroyed the work it existed to protect."""
        keep = self.ws_dir / "keep.txt"
        keep.write_text("work in progress")
        snap = self.engine.create_snapshot("before")

        with open(snap.archive_path, "wb") as fh:
            fh.write(b"not a gzip stream at all")

        self.assertFalse(self.engine.rollback(snap.id))
        self.assertTrue(keep.exists(), "the workspace was cleared before the archive was read")
        self.assertEqual(keep.read_text(), "work in progress")

    def test_rollback_restores_nested_directories(self):
        nested = self.ws_dir / "src" / "pkg"
        nested.mkdir(parents=True)
        (nested / "mod.py").write_text("original")
        snap = self.engine.create_snapshot("nested")

        (nested / "mod.py").write_text("changed")
        (self.ws_dir / "junk").mkdir()

        self.assertTrue(self.engine.rollback(snap.id))
        self.assertEqual((nested / "mod.py").read_text(), "original")
        self.assertFalse((self.ws_dir / "junk").exists())


if __name__ == "__main__":
    unittest.main()
