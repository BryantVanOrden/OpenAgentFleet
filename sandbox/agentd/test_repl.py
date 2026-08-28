"""Tests for the Persistent Python REPL engine."""

import unittest
from repl import PersistentREPL


class TestPersistentREPL(unittest.TestCase):
    def setUp(self):
        self.repl = PersistentREPL()

    def test_variable_persistence(self):
        ok, out = self.repl.execute("x = 42")
        self.assertTrue(ok)

        ok, out = self.repl.execute("x + 8")
        self.assertTrue(ok)
        self.assertIn("50", out)

    def test_function_definition_persistence(self):
        code = """
def add(a, b):
    return a + b
"""
        ok, out = self.repl.execute(code)
        self.assertTrue(ok)

        ok, out = self.repl.execute("add(10, 25)")
        self.assertTrue(ok)
        self.assertIn("35", out)

    def test_state_dictionary_persistence(self):
        ok, out = self.repl.execute("state['counter'] = 1")
        self.assertTrue(ok)

        ok, out = self.repl.execute("state['counter'] += 1; print(state['counter'])")
        self.assertTrue(ok)
        self.assertIn("2", out)

    def test_stdout_capture(self):
        ok, out = self.repl.execute("print('Hello from persistent REPL')")
        self.assertTrue(ok)
        self.assertEqual(out, "Hello from persistent REPL")

    def test_error_handling(self):
        ok, out = self.repl.execute("1 / 0")
        self.assertFalse(ok)
        self.assertIn("ZeroDivisionError", out)

    def test_reset(self):
        self.repl.execute("val = 'persistent'")
        self.repl.reset()
        ok, out = self.repl.execute("val")
        self.assertFalse(ok)
        self.assertIn("NameError", out)


if __name__ == "__main__":
    unittest.main()
