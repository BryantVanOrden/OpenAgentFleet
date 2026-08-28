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
    """Stateful Python execution environment with dynamic tool lifecycle."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.state: dict[str, Any] = {}
        self.mounted_tools: dict[str, dict[str, Any]] = {}
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
            self.mounted_tools.clear()
            self.globals = {
                "__name__": "__agentfleet_repl__",
                "__doc__": "AgentFleet Persistent Python REPL",
                "a11y": a11y,
                "capture": capture,
                "inject": inject,
                "state": self.state,
            }

    def mount_tool(
        self,
        name: str,
        description: str,
        parameters: dict[str, Any] | None,
        handler_code: str,
    ) -> tuple[bool, str]:
        """Dynamically registers a custom tool backed by Python code in the REPL."""
        if not name or not name.isidentifier():
            return False, f"invalid tool name: '{name}' (must be a valid Python identifier)"

        ok, out = self.execute(handler_code)
        if not ok:
            return False, f"failed to compile tool handler for '{name}': {out}"

        with self._lock:
            func = self.globals.get(name)
            if not callable(func):
                return False, f"tool handler code must define a callable named '{name}'"
            self.mounted_tools[name] = {
                "name": name,
                "description": description or f"Custom tool {name}",
                "parameters": parameters or {},
            }
        return True, f"mounted tool '{name}' successfully"

    def unmount_tool(self, name: str) -> tuple[bool, str]:
        """Disposes of a previously mounted tool (reversible lifecycle)."""
        with self._lock:
            if name in self.mounted_tools:
                del self.mounted_tools[name]
                self.globals.pop(name, None)
                return True, f"unmounted tool '{name}'"
            return False, f"tool '{name}' was not mounted"

    def call_tool(self, name: str, params: dict[str, Any] | None) -> tuple[bool, str]:
        """Executes a mounted tool function with given parameters."""
        with self._lock:
            if name not in self.mounted_tools:
                return False, f"tool '{name}' is not mounted"
            func = self.globals.get(name)
            if not callable(func):
                return False, f"mounted tool '{name}' is not callable"

        args = params or {}
        call_expr = f"{name}(**{repr(args)})"
        return self.execute(call_expr)

    def list_tools(self) -> list[dict[str, Any]]:
        with self._lock:
            return list(self.mounted_tools.values())

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
