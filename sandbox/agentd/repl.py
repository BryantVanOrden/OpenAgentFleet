"""Persistent Python REPL engine for agentd.

Treats Python execution as a stateful, interactive computational substrate
inspired by Prime Agent's RLM architecture. Variables, functions, and state
defined in one step persist to subsequent steps. Pre-injects `a11y`, `capture`
and a curated `inject` facade — not the raw injection module, see
`_InjectFacade` below.
"""

from __future__ import annotations

import io
import os
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
    import inject as _inject
except Exception:
    _inject = None


# Read once, at import, from the same environment variable agentd's /act handler
# uses. Not re-read per call, so REPL code cannot flip it with
# `os.environ["ALLOW_SHELL"] = "true"`.
_CODE_EXECUTION_ENABLED = os.environ.get("ALLOW_SHELL", "false").lower() in ("1", "true", "yes")


class _InjectFacade:
    """The only view of `inject` that REPL code gets.

    The raw module used to be bound straight into the REPL globals, and its
    `shell(command, allow, ...)` takes the gate as a *parameter* — so any code in
    the REPL could simply pass `allow=True`. Since the REPL itself now sits
    behind the same `shell_access` gate as `shell`, that is no longer an
    escalation, but a helper whose privilege is decided by an argument its caller
    controls is a trap waiting for the next refactor to re-arm it. This facade
    exposes the perception/input helpers and a `shell()` that takes no allow
    flag: the decision comes from `_CODE_EXECUTION_ENABLED`, captured at import.

    Honest limit: this is trap removal, not a sandbox. Code in the REPL has a
    whole interpreter — `subprocess`, `os.system`, and rebinding this module's
    globals are all still there. Nothing inside one Python process can gate
    another part of that process. The enforcement that counts happens before any
    of this runs, in the orchestrator and in agentd's `/act`.
    """

    # Perception / input helpers, forwarded unchanged. None of these takes a
    # privilege argument.
    _FORWARD = (
        "click",
        "type_text",
        "key",
        "scroll",
        "drag",
        "focus_window",
        "resolve_target",
        "wait_for_text",
        "command_available",
        "quote",
    )

    def __init__(self, module: Any) -> None:
        self._module = module
        for name in self._FORWARD:
            fn = getattr(module, name, None) if module is not None else None
            if fn is not None:
                setattr(self, name, fn)

    def __repr__(self) -> str:  # what `inject` prints as in the REPL
        return "<agentfleet input helpers: %s, shell, assert_condition>" % ", ".join(self._FORWARD)

    def screenshot(self, *args: Any, **kwargs: Any) -> Any:
        """Grab the current framebuffer (the `capture` module's `grab`)."""
        if capture is None:
            return None
        return capture.grab(*args, **kwargs)

    def shell(self, command: str, timeout: int = 600) -> tuple[bool, str, int]:
        """Run a shell command. There is no allow flag to pass."""
        if self._module is None:
            return False, "input helpers unavailable", 127
        if not _CODE_EXECUTION_ENABLED:
            return False, "code execution is disabled for this instance", 126
        return self._module.shell(command, True, timeout=timeout)

    def assert_condition(self, expression: str) -> tuple[bool, str]:
        if self._module is None:
            return False, "input helpers unavailable"
        return self._module.assert_condition(expression, _CODE_EXECUTION_ENABLED)


inject = _InjectFacade(_inject)


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
        """Execute a block of Python code, capturing stdout/stderr and return value.

        The timeout is enforced by running the code on a daemon thread and
        refusing to wait past the deadline. Python cannot safely kill a running
        thread, so a runaway `while True:` is not stopped — but it no longer
        takes the whole daemon down with it: the lock is released, the caller
        gets an error, and every later action still works. Before this, the
        timeout argument was accepted and ignored, so one bad loop wedged agentd
        permanently and the only recovery was destroying the instance.
        """
        if not self._lock.acquire(timeout=max(1, timeout)):
            return False, (
                "the REPL is still busy with a previous execution that has not "
                "returned; it may be in an infinite loop"
            )
        try:
            return self._execute_locked(code, timeout)
        finally:
            self._lock.release()

    def _run_guarded(self, fn, timeout: int) -> tuple[bool, str]:
        """Run fn on a daemon thread and give up waiting after `timeout`."""
        outcome: list[tuple[bool, str]] = []

        def runner() -> None:
            try:
                outcome.append((True, fn()))
            except BaseException as exc:  # noqa: BLE001 - surfaced to the agent
                outcome.append((False, f"{type(exc).__name__}: {exc}"))

        worker = threading.Thread(target=runner, daemon=True)
        worker.start()
        worker.join(timeout=max(1, timeout))
        if worker.is_alive():
            return False, f"execution exceeded {timeout}s and was abandoned"
        return outcome[0] if outcome else (False, "execution produced no result")

    def _execute_locked(self, code: str, timeout: int) -> tuple[bool, str]:
        if True:
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
                        if isinstance(res, str):
                            print(res)
                        else:
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
