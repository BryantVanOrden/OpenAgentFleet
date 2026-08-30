"""Input injection.

xdotool is used rather than a Python X client because it already handles the
awkward parts correctly: keyboard-map lookups for arbitrary Unicode, modifier
sequencing for chords, and per-window targeting.

Every entry point returns (ok, detail) instead of raising. The agent loop needs
"that click landed on nothing" as a *result* it can reason about, not an
exception that ends the run.
"""

from __future__ import annotations

import os
import shlex
import subprocess
import tempfile
import time

import a11y

_TIMEOUT = 15


def _run(args: list[str], timeout: int = _TIMEOUT) -> tuple[int, str]:
    try:
        out = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
        return out.returncode, (out.stdout + out.stderr).strip()
    except subprocess.TimeoutExpired:
        return 124, f"timed out after {timeout}s"
    except OSError as exc:
        return 127, str(exc)


def resolve_target(label: str, role: str | None = None) -> tuple[int, int] | None:
    """Turn an accessible label into clickable coordinates."""
    node = a11y.find(label, role)
    if node is None:
        return None
    return node.center


def _pointer_at(x: int, y: int) -> bool:
    """Is the pointer already where we are about to move it?"""
    code, out = _run(["xdotool", "getmouselocation", "--shell"])
    if code != 0:
        return False
    pos = {}
    for line in out.strip().splitlines():
        key, _, val = line.partition("=")
        pos[key.strip()] = val.strip()
    try:
        return int(pos.get("X", -1)) == x and int(pos.get("Y", -1)) == y
    except ValueError:
        return False


def click(x: int, y: int, button: int = 1, clicks: int = 1) -> tuple[bool, str]:
    # Moving to where the pointer already is stalls.
    #
    # `mousemove --sync` waits for a motion event, and moving somewhere the
    # pointer already is produces none, so it sits there for seconds and under
    # load overruns the timeout. The caller sees "mousemove failed" and the
    # agent concludes the element is unclickable -- which is what clicking the
    # same button twice in a row looks like from the inside. Nothing to do:
    # we are already there.
    if not _pointer_at(x, y):
        # No --sync. It waits for a motion event, and there are ordinary
        # situations that never produce one -- the pointer already being at the
        # target, a grab held by a menu -- in which it blocks until the timeout
        # and reports "mousemove failed: timed out after 15s". Whole runs were
        # lost to that: an agent clicking the same button twice was told the
        # second click was impossible. A plain move plus a short settle is what
        # --sync was standing in for.
        code, out = _run(["xdotool", "mousemove", str(x), str(y)], timeout=5)
        if code != 0:
            return False, f"mousemove failed: {out}"
        time.sleep(0.05)
    args = ["xdotool", "click", "--repeat", str(clicks), "--delay", "80", str(button)]
    code, out = _run(args)
    if code != 0:
        return False, f"click failed: {out}"
    # Give the UI a moment to react before the next observation is taken.
    time.sleep(0.35)
    return True, f"clicked {x},{y}"


def type_text(text: str) -> tuple[bool, str]:
    # --clearmodifiers stops a held Shift from a previous chord corrupting input.
    code, out = _run(["xdotool", "type", "--clearmodifiers", "--delay", "25", "--", text],
                     timeout=max(_TIMEOUT, len(text) // 20 + 10))
    if code != 0:
        return False, f"type failed: {out}"
    time.sleep(0.2)
    return True, f"typed {len(text)} characters"


def key(combo: str) -> tuple[bool, str]:
    code, out = _run(["xdotool", "key", "--clearmodifiers", combo])
    if code != 0:
        return False, f"key failed: {out}"
    time.sleep(0.25)
    return True, f"pressed {combo}"


def scroll(x: int, y: int, amount: int) -> tuple[bool, str]:
    button = 4 if amount > 0 else 5  # 4 = up, 5 = down
    _run(["xdotool", "mousemove", str(x), str(y)], timeout=5)  # see click()
    code, out = _run(["xdotool", "click", "--repeat", str(abs(amount)), "--delay", "60", str(button)])
    if code != 0:
        return False, f"scroll failed: {out}"
    time.sleep(0.25)
    return True, f"scrolled {amount}"


def drag(x1: int, y1: int, x2: int, y2: int) -> tuple[bool, str]:
    steps = [
        ["xdotool", "mousemove", str(x1), str(y1)],
        ["xdotool", "mousedown", "1"],
        # An intermediate move makes drag-and-drop work in toolkits that ignore a
        # single jump because they never see a motion event.
        ["xdotool", "mousemove", str((x1 + x2) // 2), str((y1 + y2) // 2)],
        ["xdotool", "mousemove", str(x2), str(y2)],
        ["xdotool", "mouseup", "1"],
    ]
    for args in steps:
        code, out = _run(args)
        if code != 0:
            _run(["xdotool", "mouseup", "1"])  # never leave the button held
            return False, f"drag failed at {args[1]}: {out}"
        time.sleep(0.08)
    time.sleep(0.3)
    return True, f"dragged {x1},{y1} -> {x2},{y2}"


def focus_window(title: str) -> tuple[bool, str]:
    code, out = _run(["xdotool", "search", "--name", title])
    if code != 0 or not out:
        return False, f"no window matching {title!r}"
    window_id = out.splitlines()[0].strip()
    code, out = _run(["xdotool", "windowactivate", "--sync", window_id])
    if code != 0:
        # windowactivate needs a window manager; raise is the fallback.
        code, out = _run(["xdotool", "windowraise", window_id])
        if code != 0:
            return False, f"could not focus {title!r}: {out}"
    time.sleep(0.3)
    return True, f"focused {title!r}"


def shell(command: str, allow: bool, timeout: int = 600) -> tuple[bool, str, int]:
    """Run a shell command as the unprivileged agent user.

    Gated twice: the orchestrator refuses the action if the instance did not opt
    in, and agentd refuses again here. A single check would be one bug away from
    a sandbox with an unexpected shell.

    Output goes to temporary FILES rather than pipes, which is not a style
    choice. With `capture_output=True`, Python waits for EOF on the pipe, and a
    backgrounded grandchild inherits that pipe and holds it open — so
    `firefox &` would block for the full timeout instead of returning at once.
    An agent launching a GUI app is the single most common shell action there
    is, so it has to return immediately.
    """
    if not allow:
        return False, "shell execution is disabled for this instance", 126

    env = dict(os.environ)
    env["DISPLAY"] = os.environ.get("DISPLAY", ":1")
    # Long output would otherwise blow the model's context in one step.
    workdir = os.path.expanduser("~/work")
    os.makedirs(workdir, exist_ok=True)

    try:
        with tempfile.TemporaryFile(mode="w+", encoding="utf-8", errors="replace") as sink:
            proc = subprocess.run(
                ["/bin/bash", "-lc", command],
                stdin=subprocess.DEVNULL,
                stdout=sink,
                stderr=subprocess.STDOUT,
                timeout=timeout,
                cwd=workdir,
                env=env,
                # Detach from agentd's process group so a runaway child cannot
                # take the control plane down with it.
                start_new_session=True,
            )
            sink.seek(0)
            combined = sink.read().strip()
    except subprocess.TimeoutExpired:
        return False, f"command timed out after {timeout}s", 124
    except OSError as exc:
        return False, str(exc), 127

    if len(combined) > 8000:
        combined = combined[:2000] + "\n...[truncated]...\n" + combined[-6000:]
    return proc.returncode == 0, combined, proc.returncode


def assert_condition(expression: str, allow_shell: bool) -> tuple[bool, str]:
    """Verify a real outcome.

    Two forms:
      file:<path>[:<min_bytes>]   the file exists and is at least that big
      <shell test>                any command; exit zero means the assert holds
    """
    expression = expression.strip()
    if expression.startswith("file:"):
        parts = expression.split(":")
        path = os.path.expanduser(parts[1]) if len(parts) > 1 else ""
        min_bytes = int(parts[2]) if len(parts) > 2 and parts[2].isdigit() else 1
        if not path:
            return False, "no path in assertion"
        if not os.path.exists(path):
            return False, f"{path} does not exist"
        size = os.path.getsize(path)
        if size < min_bytes:
            return False, f"{path} is {size} bytes, expected at least {min_bytes}"
        return True, f"{path} exists ({size} bytes)"

    ok, out, code = shell(expression, allow_shell, timeout=120)
    if not allow_shell:
        return False, "shell assertions need shell access enabled"
    return ok, f"exit {code}: {out[:400]}"


def wait_for_text(needle: str, timeout: int) -> tuple[bool, str]:
    """Poll the accessibility text until `needle` appears."""
    needle_l = needle.lower()
    deadline = time.time() + max(1, timeout)
    checks = 0
    while time.time() < deadline:
        haystack = a11y.flatten_text().lower()
        checks += 1
        if needle_l in haystack:
            return True, f"found after {checks} checks"
        time.sleep(1.5)
    return False, f"not found after {checks} checks ({timeout}s)"


def command_available(name: str) -> bool:
    code, _ = _run(["which", name], timeout=5)
    return code == 0


def quote(value: str) -> str:
    return shlex.quote(value)
