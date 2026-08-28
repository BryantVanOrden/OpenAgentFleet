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

    def __init__(self) -> None:
        # Bound methods only. The raw module is deliberately NOT kept as an
        # attribute — `inject._module.shell(cmd, True)` would hand the parameter
        # back to the caller and undo the whole point of the facade.
        for name in self._FORWARD:
            fn = getattr(_inject, name, None) if _inject is not None else None
            if fn is not None:
                setattr(self, name, fn)

    def __repr__(self) -> str:  # what `inject` prints as in the REPL
        return "<agentfleet input helpers: %s, screenshot, shell, assert_condition>" % ", ".join(
            self._FORWARD
        )

    def screenshot(self, *args: Any, **kwargs: Any) -> Any:
        """Grab the current framebuffer (the `capture` module's `grab`)."""
        if capture is None:
            return None
        return capture.grab(*args, **kwargs)

    def shell(self, command: str, timeout: int = 600) -> tuple[bool, str, int]:
        """Run a shell command. There is no allow flag to pass."""
        if _inject is None:
            return False, "input helpers unavailable", 127
        if not _CODE_EXECUTION_ENABLED:
            return False, "code execution is disabled for this instance", 126
        return _inject.shell(command, True, timeout=timeout)

    def assert_condition(self, expression: str) -> tuple[bool, str]:
        if _inject is None:
            return False, "input helpers unavailable"
        return _inject.assert_condition(expression, _CODE_EXECUTION_ENABLED)


inject = _InjectFacade()


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

    def _fresh_globals(self, state: dict[str, Any]) -> dict[str, Any]:
        """A clean execution namespace bound to `state`."""
        return {
            "__name__": "__agentfleet_repl__",
            "__doc__": "AgentFleet Persistent Python REPL",
            "a11y": a11y,
            "capture": capture,
            "inject": inject,
            "state": state,
        }

    def reset(self) -> None:
        with self._lock:
            self.state.clear()
            self.mounted_tools.clear()
            self.globals = self._fresh_globals(self.state)

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
        """Execute a block of Python code, capturing output and the return value.

        Three things this has to get right, all of which it previously did not:

        1. **The timeout is real.** The work runs on a daemon thread and the
           caller stops waiting at the deadline. Python cannot safely kill a
           running thread, so a runaway `while True:` is abandoned rather than
           stopped — but it no longer hangs the caller, and agentd keeps serving
           every other action.
        2. **Output capture is local, not global.** An earlier version swapped
           `sys.stdout` for a StringIO and restored it in a `finally`. When the
           thread never returns, that `finally` never runs and agentd's own
           stdout stays swallowed for the life of the process. Instead the
           executed code gets its own `print` bound into its globals, so nothing
           process-wide is touched.
        3. **A wedged run does not silently corrupt later ones.** The abandoned
           thread still owns the shared globals dict, so the lock stays held and
           subsequent calls are refused with a clear message rather than racing
           it.
        """
        # Short acquire: if a previous run is wedged there is no point queueing
        # behind it for the full timeout, and the caller wants an answer.
        if not self._lock.acquire(timeout=min(5, max(1, timeout))):
            return False, (
                "the REPL is busy with a previous execution that has not returned "
                "(most likely an infinite loop). Its Python state is not safe to "
                "share, so further code execution is refused on this instance. "
                "Every other action still works; recreate the instance to get a "
                "clean interpreter."
            )

        completed = False
        try:
            result, completed = self._run_guarded(code, timeout)
            return result
        finally:
            if not completed:
                # The worker is still running and owns the old namespace, so we
                # cannot reuse it — but refusing every later call would let one
                # bad loop kill the instance's REPL until it is recreated.
                # Instead the runaway keeps the dict it is mutating, and new
                # work gets a clean one. The two threads then share nothing, so
                # releasing the lock is safe. Cost: variables and mounted tools
                # from before the runaway are gone, which is the same outcome as
                # a reset and far better than a dead interpreter.
                self._recycle_after_runaway()
            self._lock.release()

    def _recycle_after_runaway(self) -> None:
        """Abandon the namespace an unstoppable execution is still holding."""
        self.state = {}
        self.mounted_tools = {}
        self.globals = self._fresh_globals(self.state)

    def _run_guarded(self, code: str, timeout: int) -> tuple[tuple[bool, str], bool]:
        """Run one execution on a daemon thread.

        Returns ((ok, output), completed). `completed` is False when the
        deadline passed and the worker was abandoned.
        """
        outcome: list[tuple[bool, str]] = []

        def runner() -> None:
            try:
                outcome.append(self._execute_once(code))
            except BaseException as exc:  # noqa: BLE001 - surfaced to the agent
                outcome.append((False, f"{type(exc).__name__}: {exc}"))

        worker = threading.Thread(
            target=runner, daemon=True, name="agentfleet-repl-exec"
        )
        worker.start()
        worker.join(timeout=max(1, timeout))

        if worker.is_alive():
            return (
                False,
                f"execution exceeded {timeout}s and was abandoned; the REPL is "
                f"now unusable on this instance",
            ), False

        if not outcome:
            return (False, "execution produced no result"), True
        return outcome[0], True

    def _execute_once(self, code: str) -> tuple[bool, str]:
        """Compile and run one snippet, capturing what it prints.

        Output is captured by giving the snippet its own `print`, not by
        redirecting `sys.stdout` — see the note in `execute`. Code that reaches
        for `sys.stdout` directly will bypass capture and write to agentd's log;
        that is an acceptable trade for never losing the daemon's own output.
        """
        buf = io.StringIO()
        start_time = time.time()
        error_occured = False

        def captured_print(*args: Any, **kwargs: Any) -> None:
            kwargs.pop("file", None)
            print(*args, file=buf, **kwargs)

        run_globals = self.globals
        previous_print = run_globals.get("print")
        run_globals["print"] = captured_print

        try:
            # An expression first, so `2 + 2` reports 4 rather than nothing.
            try:
                expr_code = compile(code, "<repl>", "eval")
                res = eval(expr_code, run_globals)
                if res is not None:
                    captured_print(res if isinstance(res, str) else repr(res))
            except SyntaxError:
                exec_code = compile(code, "<repl>", "exec")
                exec(exec_code, run_globals)
        except Exception:
            error_occured = True
            traceback.print_exc(file=buf)
        finally:
            if previous_print is None:
                run_globals.pop("print", None)
            else:
                run_globals["print"] = previous_print

        combined = buf.getvalue().strip()
        duration_ms = int((time.time() - start_time) * 1000)
        if not combined:
            combined = f"ok ({duration_ms}ms, no output)"
        if len(combined) > 8000:
            combined = combined[:2000] + "\n...[truncated]...\n" + combined[-6000:]

        return not error_occured, combined


REPL = PersistentREPL()
