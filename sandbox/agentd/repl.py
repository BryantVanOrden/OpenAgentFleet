"""Persistent Python REPL engine for agentd.

Treats Python execution as a stateful, interactive computational substrate
inspired by Prime Agent's RLM architecture. Variables, functions, and state
defined in one step persist to subsequent steps. Pre-injects a11y, capture,
and inject helper utilities.
"""

from __future__ import annotations

import io
import sys
import threading
import time
import traceback
from typing import Any

try:
    import a11y
except Exception:
    a11y = None

try:
    import capture
except Exception:
    capture = None

try:
    import inject
except Exception:
    inject = None


class PersistentREPL:
    """Stateful Python execution environment."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.state: dict[str, Any] = {}
        self.globals: dict[str, Any] = {
            "__name__": "__agentfleet_repl__",
            "__doc__": "AgentFleet Persistent Python REPL",
            "a11y": a11y,
            "capture": capture,
            "inject": inject,
            "state": self.state,
        }

    def reset(self) -> None:
        with self._lock:
            self.state.clear()
            self.globals = {
                "__name__": "__agentfleet_repl__",
                "__doc__": "AgentFleet Persistent Python REPL",
                "a11y": a11y,
                "capture": capture,
                "inject": inject,
                "state": self.state,
            }

    def execute(self, code: str, timeout: int = 60) -> tuple[bool, str]:
        """Execute a block of Python code, capturing stdout/stderr and return value."""
        with self._lock:
            stdout_buf = io.StringIO()
            stderr_buf = io.StringIO()
            old_stdout = sys.stdout
            old_stderr = sys.stderr

            error_occured = False
            result_str = ""
            start_time = time.time()

            try:
                sys.stdout = stdout_buf
                sys.stderr = stderr_buf

                # Try parsing as a single expression first (to capture its eval return)
                try:
                    expr_code = compile(code, "<repl>", "eval")
                    res = eval(expr_code, self.globals)
                    if res is not None:
                        print(repr(res))
                except SyntaxError:
                    # Multi-statement or block: compile as exec
                    exec_code = compile(code, "<repl>", "exec")
                    exec(exec_code, self.globals)

            except Exception:
                error_occured = True
                traceback.print_exc(file=stderr_buf)
            finally:
                sys.stdout = old_stdout
                sys.stderr = old_stderr

            stdout_val = stdout_buf.getvalue()
            stderr_val = stderr_buf.getvalue()

            combined = ""
            if stdout_val:
                combined += stdout_val
            if stderr_val:
                if combined:
                    combined += "\n"
                combined += stderr_val

            combined = combined.strip()
            duration_ms = int((time.time() - start_time) * 1000)

            if not combined:
                combined = f"ok ({duration_ms}ms, no output)"

            if len(combined) > 8000:
                combined = combined[:2000] + "\n...[truncated]...\n" + combined[-6000:]

            return not error_occured, combined


REPL = PersistentREPL()
