"""The host writes colleagues' newer versions of shared files into the agent's folder, and nowhere else."""

import tempfile
import unittest
from pathlib import Path

from agentfleet import host


class IncomingFilesTests(unittest.TestCase):
    def test_writes_inside_the_folder_only(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "work"
            root.mkdir()
            (root / "src").mkdir()
            (root / "src" / "lib.js").write_text("old", encoding="utf-8")
            cls = next(v for v in vars(host).values() if isinstance(v, type) and hasattr(v, "_write_incoming"))
            written = cls._write_incoming(root, [
                {"path": "src/lib.js", "content": "export const fixed = true"},
                {"path": "notes/new.md", "content": "# New"},
                {"path": "../escape.txt", "content": "no"},
                {"path": "/etc/evil.txt", "content": "no"},
                {"path": "C:/Windows/evil.txt", "content": "no"},
                {"path": "", "content": "no"},
            ])
            self.assertEqual(sorted(written), ["notes/new.md", "src/lib.js"])
            self.assertEqual((root / "src" / "lib.js").read_text(encoding="utf-8"), "export const fixed = true")
            self.assertEqual((root / "notes" / "new.md").read_text(encoding="utf-8"), "# New")
            self.assertFalse((Path(d) / "escape.txt").exists())
            self.assertFalse((root / "etc").exists(), "an absolute path is refused, not made relative")


if __name__ == "__main__":
    unittest.main()
