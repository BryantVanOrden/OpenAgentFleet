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

if __name__ == "__main__":
    unittest.main()
