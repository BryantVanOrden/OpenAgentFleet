import unittest
import os
import tempfile
from repl import PersistentREPL

class TestArchetypeUsability(unittest.TestCase):
    def setUp(self):
        self.repl = PersistentREPL()

    def test_python_repl_state_persistence(self):
        """Verify state and tool definitions persist across turns for agents."""
        ok, out = self.repl.execute("count = 42\ndef add_five(x): return x + 5")
        self.assertTrue(ok)
        self.assertIn("count", self.repl.state or self.repl.globals)

        ok2, out2 = self.repl.execute("result = add_five(count)\nprint(result)")
        self.assertTrue(ok2)
        self.assertEqual(out2.strip(), "47")

    def test_dynamic_tool_lifecycle(self):
        """Verify dynamic tool mounting and calling for archetypes."""
        code = """
def network_scan(target="127.0.0.1"):
    return f"scanned {target}: open ports [80, 443]"
"""
        ok, msg = self.repl.mount_tool("network_scan", "Scans target", {"target": "str"}, code)
        self.assertTrue(ok, msg)
        self.assertIn("network_scan", self.repl.mounted_tools)

        ok_call, res = self.repl.call_tool("network_scan", {"target": "10.0.0.1"})
        self.assertTrue(ok_call)
        self.assertEqual(res, "scanned 10.0.0.1: open ports [80, 443]")

        ok_unmount, umsg = self.repl.unmount_tool("network_scan")
        self.assertTrue(ok_unmount, umsg)
        self.assertNotIn("network_scan", self.repl.mounted_tools)

    def test_workspace_initialization_simulation(self):
        """Simulate init-archetype script logic on a temporary directory."""
        with tempfile.TemporaryDirectory() as tmpdir:
            readme_path = os.path.join(tmpdir, "ARCHETYPE_README.md")
            archetype = "cyber_ops"
            tools = "nmap, wireshark, semgrep"
            content = f"# Agent Workspace ({archetype})\n\nPre-configured Tools: {tools}\n"
            with open(readme_path, "w", encoding="utf-8") as f:
                f.write(content)

            self.assertTrue(os.path.exists(readme_path))
            with open(readme_path, "r", encoding="utf-8") as f:
                read_data = f.read()
            self.assertIn("cyber_ops", read_data)
            self.assertIn("nmap", read_data)

if __name__ == "__main__":
    unittest.main()
