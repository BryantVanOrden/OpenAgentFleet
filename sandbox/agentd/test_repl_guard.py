"""Timeout enforcement and tool bookkeeping for the persistent REPL.

`PersistentREPL.execute` documents a hard promise: a runaway snippet is
abandoned after `timeout` seconds, the lock is released, and every later action
still works. That promise is what keeps one bad `while True:` from wedging
agentd permanently — the previous failure mode where the only recovery was
destroying the instance.

The runaway case is exercised in a CHILD PROCESS on purpose. Python cannot kill
a thread, so if the guard is not working the runaway loop lives for the rest of
the interpreter's life; worse, `_execute_locked` swaps `sys.stdout` for a
StringIO and only restores it in a `finally` that never runs, so an in-process
runaway would also silently swallow this test runner's own output. A child
process is the only way to ask the question without betting the suite on the
answer.
"""

import json
import os
import subprocess
import sys
import unittest

from repl import PersistentREPL, _InjectFacade

HERE = os.path.dirname(os.path.abspath(__file__))

# Runs in a throwaway interpreter. Writes to sys.__stdout__ rather than
# sys.stdout because the module under test may have replaced the latter.
_CHILD = r"""
import json, sys, threading
sys.path.insert(0, {here!r})
from repl import PersistentREPL

repl = PersistentREPL()
report = {{}}

ok, out = repl.execute("6 * 7", timeout=5)
report["fast_ok"] = ok
report["fast_out"] = out

done = []
worker = threading.Thread(
    target=lambda: done.append(repl.execute("while True: pass", timeout=1)),
    daemon=True,
)
worker.start()
worker.join({join!r})

report["runaway_returned"] = bool(done)
report["runaway_result"] = list(done[0]) if done else None

ok, out = repl.execute("2 + 2", timeout=1)
report["later_ok"] = ok
report["later_out"] = out

sys.__stdout__.write("RESULT:" + json.dumps(report) + "\n")
sys.__stdout__.flush()
"""


def _run_runaway_probe(join_seconds=3.0, wall_clock=12.0):
    src = _CHILD.format(here=HERE, join=join_seconds)
    proc = subprocess.run(
        [sys.executable, "-c", src],
        capture_output=True,
        text=True,
        timeout=wall_clock,
    )
    for line in proc.stdout.splitlines():
        if line.startswith("RESULT:"):
            return json.loads(line[len("RESULT:"):])
    raise AssertionError(
        "REPL probe produced no result.\nstdout=%r\nstderr=%r" % (proc.stdout, proc.stderr)
    )


class TestExecuteTimeout(unittest.TestCase):
    """One probe, three assertions — spawning the child once keeps it quick."""

    @classmethod
    def setUpClass(cls):
        try:
            cls.report = _run_runaway_probe()
            cls.probe_error = None
        except Exception as exc:  # subprocess.TimeoutExpired included
            cls.report = None
            cls.probe_error = exc

    def setUp(self):
        if self.report is None:
            self.fail(
                "the REPL runaway probe never returned, so execute() blocks the "
                "whole interpreter: %r" % (self.probe_error,)
            )

    def test_fast_snippet_returns_its_value(self):
        self.assertTrue(self.report["fast_ok"], self.report["fast_out"])
        self.assertIn("42", self.report["fast_out"])

    def test_runaway_snippet_returns_a_timeout_error_instead_of_hanging(self):
        self.assertTrue(
            self.report["runaway_returned"],
            "execute('while True: pass', timeout=1) never returned. "
            "PersistentREPL.execute (repl.py:211) calls _execute_locked directly "
            "on the calling thread; the guard thread helper _run_guarded "
            "(repl.py:215) is dead code and is never invoked, so the timeout "
            "argument is still ignored.",
        )
        ok, out = self.report["runaway_result"]
        self.assertFalse(ok, "a runaway loop must not report success")
        self.assertRegex(out.lower(), r"exceed|timeout|abandon")

    def test_a_later_call_still_works_after_a_runaway(self):
        self.assertTrue(
            self.report["later_ok"],
            "the REPL never recovered after a runaway snippet: %r. The lock taken "
            "in execute() (repl.py:205) is held by the wedged call forever, so "
            "every subsequent action is refused. See repl.py:211/215."
            % (self.report["later_out"],),
        )
        self.assertIn("4", self.report["later_out"])


class TestExecuteLockIsReleased(unittest.TestCase):
    """Well-behaved snippets must always hand the lock back."""

    def setUp(self):
        self.repl = PersistentREPL()

    def test_successful_call_releases_the_lock(self):
        self.repl.execute("1 + 1", timeout=5)
        self.assertTrue(self.repl._lock.acquire(timeout=1))
        self.repl._lock.release()

    def test_raising_snippet_releases_the_lock(self):
        ok, out = self.repl.execute("1 / 0", timeout=5)
        self.assertFalse(ok)
        self.assertIn("ZeroDivisionError", out)
        self.assertTrue(self.repl._lock.acquire(timeout=1))
        self.repl._lock.release()

    def test_syntax_error_releases_the_lock_and_is_reported(self):
        ok, out = self.repl.execute("def (:", timeout=5)
        self.assertFalse(ok)
        self.assertIn("SyntaxError", out)
        self.assertTrue(self.repl._lock.acquire(timeout=1))
        self.repl._lock.release()

    def test_stdout_is_restored_after_a_raising_snippet(self):
        before = sys.stdout
        self.repl.execute("raise RuntimeError('boom')", timeout=5)
        self.assertIs(sys.stdout, before)
        self.assertIs(sys.stderr, sys.stderr)


class TestExecuteOutputShaping(unittest.TestCase):
    def setUp(self):
        self.repl = PersistentREPL()

    def test_expression_value_is_returned_not_just_printed(self):
        ok, out = self.repl.execute("[1, 2, 3]")
        self.assertTrue(ok)
        self.assertEqual(out, "[1, 2, 3]")

    def test_string_expression_is_not_double_quoted(self):
        ok, out = self.repl.execute("'hello'")
        self.assertTrue(ok)
        self.assertEqual(out, "hello")

    def test_none_expression_produces_the_no_output_marker(self):
        ok, out = self.repl.execute("None")
        self.assertTrue(ok)
        self.assertIn("no output", out)

    def test_statement_block_reports_no_output(self):
        ok, out = self.repl.execute("a = 1\nb = 2")
        self.assertTrue(ok)
        self.assertIn("no output", out)

    def test_long_output_is_truncated_with_a_marker(self):
        ok, out = self.repl.execute("print('x' * 20000)")
        self.assertTrue(ok)
        self.assertIn("[truncated]", out)
        self.assertLess(len(out), 9000)

    def test_stdout_and_stderr_are_both_captured(self):
        code = "import sys\nprint('to out')\nprint('to err', file=sys.stderr)"
        ok, out = self.repl.execute(code)
        self.assertTrue(ok)
        self.assertIn("to out", out)
        self.assertIn("to err", out)


class TestToolBookkeeping(unittest.TestCase):
    def setUp(self):
        self.repl = PersistentREPL()

    def _mount(self, name="probe", body=None):
        body = body or "def %s(v=1):\n    return v * 2\n" % name
        return self.repl.mount_tool(name, "doubles v", {"v": "int"}, body)

    def test_mount_registers_metadata_and_the_callable(self):
        ok, msg = self._mount()
        self.assertTrue(ok, msg)
        self.assertIn("probe", self.repl.mounted_tools)
        entry = self.repl.mounted_tools["probe"]
        self.assertEqual(entry["name"], "probe")
        self.assertEqual(entry["description"], "doubles v")
        self.assertEqual(entry["parameters"], {"v": "int"})
        self.assertTrue(callable(self.repl.globals["probe"]))

    def test_missing_description_gets_a_generated_one(self):
        ok, _ = self.repl.mount_tool("probe", "", None, "def probe():\n    return 1\n")
        self.assertTrue(ok)
        self.assertEqual(self.repl.mounted_tools["probe"]["description"], "Custom tool probe")
        self.assertEqual(self.repl.mounted_tools["probe"]["parameters"], {})

    def test_invalid_names_are_rejected_before_any_code_runs(self):
        for bad in ("", "has space", "9leading", "with-dash", "def"):
            with self.subTest(name=bad):
                ok, msg = self.repl.mount_tool(bad, "d", {}, "x = 1")
                self.assertFalse(ok)
                self.assertEqual(self.repl.mounted_tools, {})
        # 'def' is a keyword but still a valid identifier, so it mounts far
        # enough to fail on the missing callable rather than the name check.
        self.assertNotIn("def", self.repl.mounted_tools)

    def test_handler_that_defines_nothing_callable_is_rejected(self):
        ok, msg = self.repl.mount_tool("probe", "d", {}, "probe = 42")
        self.assertFalse(ok)
        self.assertIn("callable", msg)
        self.assertEqual(self.repl.mounted_tools, {})

    def test_handler_that_fails_to_compile_is_rejected(self):
        ok, msg = self.repl.mount_tool("probe", "d", {}, "def probe(:\n")
        self.assertFalse(ok)
        self.assertIn("failed to compile", msg)
        self.assertEqual(self.repl.mounted_tools, {})

    def test_handler_that_raises_at_definition_time_is_rejected(self):
        ok, msg = self.repl.mount_tool("probe", "d", {}, "raise ValueError('nope')")
        self.assertFalse(ok)
        self.assertIn("failed to compile", msg)
        self.assertEqual(self.repl.mounted_tools, {})

    def test_remount_replaces_metadata_without_duplicating_the_entry(self):
        self._mount()
        ok, _ = self.repl.mount_tool(
            "probe", "triples v", {"v": "float"}, "def probe(v=1):\n    return v * 3\n"
        )
        self.assertTrue(ok)
        self.assertEqual(len(self.repl.list_tools()), 1)
        self.assertEqual(self.repl.mounted_tools["probe"]["description"], "triples v")
        ok, out = self.repl.call_tool("probe", {"v": 5})
        self.assertTrue(ok, out)
        self.assertIn("15", out)

    def test_unmount_removes_metadata_and_the_global(self):
        self._mount()
        ok, msg = self.repl.unmount_tool("probe")
        self.assertTrue(ok)
        self.assertIn("unmounted", msg)
        self.assertEqual(self.repl.mounted_tools, {})
        self.assertNotIn("probe", self.repl.globals)

    def test_unmount_of_an_unknown_tool_is_a_clean_failure(self):
        ok, msg = self.repl.unmount_tool("never-was")
        self.assertFalse(ok)
        self.assertIn("was not mounted", msg)

    def test_unmount_is_idempotent_after_the_first_call(self):
        self._mount()
        self.assertTrue(self.repl.unmount_tool("probe")[0])
        self.assertFalse(self.repl.unmount_tool("probe")[0])

    def test_call_tool_passes_keyword_parameters(self):
        self.repl.mount_tool(
            "combine",
            "join",
            {"a": "str", "b": "str"},
            "def combine(a, b):\n    return a + '|' + b\n",
        )
        ok, out = self.repl.call_tool("combine", {"a": "left", "b": "right"})
        self.assertTrue(ok, out)
        self.assertIn("left|right", out)

    def test_call_tool_with_no_params_uses_defaults(self):
        self._mount()
        ok, out = self.repl.call_tool("probe", None)
        self.assertTrue(ok, out)
        self.assertIn("2", out)

    def test_call_tool_surfaces_handler_exceptions_as_failures(self):
        self.repl.mount_tool(
            "boom", "d", {}, "def boom():\n    raise RuntimeError('inside')\n"
        )
        ok, out = self.repl.call_tool("boom", {})
        self.assertFalse(ok)
        self.assertIn("RuntimeError", out)
        # Still mounted: a failing call is not an unmount.
        self.assertIn("boom", self.repl.mounted_tools)

    def test_call_of_an_unmounted_tool_fails_cleanly(self):
        ok, msg = self.repl.call_tool("ghost", {})
        self.assertFalse(ok)
        self.assertIn("not mounted", msg)

    def test_call_when_the_global_was_clobbered_fails_cleanly(self):
        self._mount()
        self.repl.execute("probe = 'not a function'")
        ok, msg = self.repl.call_tool("probe", {})
        self.assertFalse(ok)
        self.assertIn("not callable", msg)

    def test_list_tools_returns_a_snapshot_not_the_live_dict(self):
        self._mount()
        snapshot = self.repl.list_tools()
        self.repl.unmount_tool("probe")
        self.assertEqual(len(snapshot), 1)
        self.assertEqual(self.repl.list_tools(), [])

    def test_reset_clears_tools_state_and_globals(self):
        self._mount()
        self.repl.execute("state['k'] = 'v'")
        self.repl.execute("leftover = 1")
        self.repl.reset()
        self.assertEqual(self.repl.mounted_tools, {})
        self.assertEqual(self.repl.state, {})
        self.assertNotIn("leftover", self.repl.globals)
        self.assertNotIn("probe", self.repl.globals)

    def test_reset_keeps_the_state_mapping_wired_into_globals(self):
        self.repl.reset()
        self.repl.execute("state['after'] = 1")
        self.assertEqual(self.repl.state.get("after"), 1)

    def test_instances_do_not_share_tool_registries(self):
        other = PersistentREPL()
        self._mount()
        self.assertEqual(other.list_tools(), [])


class TestInjectFacade(unittest.TestCase):
    """The facade must not hand the privilege decision back to REPL code."""

    def setUp(self):
        self.facade = _InjectFacade()

    def test_shell_takes_no_allow_flag(self):
        import inspect

        params = list(inspect.signature(self.facade.shell).parameters)
        self.assertEqual(params, ["command", "timeout"])
        self.assertNotIn("allow", params)

    def test_raw_inject_module_is_not_reachable_as_an_attribute(self):
        for attr in vars(self.facade).values():
            self.assertFalse(
                getattr(attr, "__name__", "") == "inject",
                "the raw inject module leaked onto the facade",
            )

    def test_shell_is_refused_when_code_execution_is_disabled(self):
        # ALLOW_SHELL is unset in the test environment, and the gate is read
        # once at import, so this is the default posture.
        import repl as repl_module

        if repl_module._CODE_EXECUTION_ENABLED:
            self.skipTest("ALLOW_SHELL is enabled in this environment")
        ok, out, code = self.facade.shell("echo hi")
        self.assertFalse(ok)
        self.assertIn(code, (126, 127))

    def test_repr_lists_the_forwarded_helpers(self):
        text = repr(self.facade)
        for name in ("click", "type_text", "key", "shell", "screenshot"):
            self.assertIn(name, text)

    def test_screenshot_degrades_when_capture_is_unavailable(self):
        import repl as repl_module

        if repl_module.capture is None:
            self.assertIsNone(self.facade.screenshot())
        else:
            self.assertTrue(callable(repl_module.capture.grab))


if __name__ == "__main__":
    unittest.main()
