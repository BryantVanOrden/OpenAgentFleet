"""Agent CLIs that `fleetctl host` runs for external agents.

An external agent -- Claude Code, Codex or Hermes -- is a CLI on this PC. The
orchestrator hands the host an ``agent_run`` job: which CLI, which folder, the
brief, and the session to resume. The host runs the CLI non-interactively in
that folder, streams what it is doing back as progress, and posts the final
answer with token usage and the session id for next time. Stopping the run in
the console stops the process here.

The command lines follow the CLIs' documented non-interactive modes:

    claude --print --output-format stream-json --verbose [--resume ID]   (prompt on stdin)
    codex exec --json [...] [resume ID] -                                (prompt on stdin)
    hermes chat -q PROMPT --source tool [--resume ID]
"""

from __future__ import annotations

import json
import os
import re
import shutil
import signal
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Optional

RUNTIMES = {
    "claude_code": "claude",
    "codex": "codex",
    "hermes": "hermes",
}

LABELS = {"claude_code": "Claude Code", "codex": "Codex", "hermes": "Hermes"}

# Variables a parent Claude Code session sets. A CLI started with them thinks
# it is nested inside that session and behaves differently.
_STRIP_ENV = ("CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION", "CLAUDE_CODE_PARENT_SESSION", "CLAUDE_CODE_SSE_PORT")


def detect() -> list[str]:
    """The agent CLIs installed on this PC, as runtime names."""
    return [runtime for runtime in RUNTIMES if resolve(runtime) is not None]


def resolve(runtime: str) -> Optional[list[str]]:
    """The command that starts a runtime's CLI, or None when it is missing.

    ``AGENTFLEET_CLAUDE_CODE_BIN`` and friends override the binary, which is
    how the tests put a fake CLI in its place.
    """
    override = os.environ.get(f"AGENTFLEET_{runtime.upper()}_BIN")
    binary = override or RUNTIMES.get(runtime)
    if not binary:
        return None
    # An override naming a file is taken as it is: on Windows which() skips a
    # path whose extension is not in PATHEXT, such as the tests' fake .py CLIs.
    path = binary if override and os.path.isfile(override) else shutil.which(binary)
    if not path:
        return None
    # A Python script, as the tests use, runs under this interpreter.
    if path.endswith(".py"):
        return [sys.executable, path]
    # npm installs Windows CLIs as .cmd shims, which CreateProcess will not
    # start on its own.
    if os.name == "nt" and path.lower().endswith((".cmd", ".bat")):
        return ["cmd.exe", "/d", "/s", "/c", path]
    return [path]


@dataclass
class Result:
    answer: str = ""
    session_id: str = ""
    model: str = ""
    input_tokens: int = 0
    output_tokens: int = 0
    cached_tokens: int = 0
    cost_usd: float = 0.0
    is_error: bool = False
    error: str = ""
    exit_code: int = 0
    files: list[dict[str, str]] = field(default_factory=list)
    unshared: list[str] = field(default_factory=list)

    def to_json(self) -> str:
        return json.dumps({k: v for k, v in self.__dict__.items() if v or k == "answer"})


# What the fleet is sent of an agent's folder when it finishes: the text files
# it created or changed, so a colleague on another machine can read them. A
# desktop colleague cannot reach this PC, and without this the file an agent
# was asked to write was invisible to whoever had to check it.
SKIP_DIRS = {".git", ".hg", ".svn", "node_modules", "__pycache__", ".venv", "venv", ".next", ".cache",
             ".pytest_cache", ".mypy_cache", ".tox", "target", ".gradle", ".idea", ".vscode"}
MAX_SCAN = 20000
MAX_FILES = 20
MAX_FILE_BYTES = 256 * 1024
MAX_TOTAL_BYTES = 1024 * 1024


def snapshot(root: str) -> Optional[dict[str, tuple[int, int]]]:
    """Path -> (mtime_ns, size) for the files under root, or None when the
    folder is too big to be worth watching."""
    out: dict[str, tuple[int, int]] = {}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS and not d.startswith(".")]
        for name in filenames:
            full = os.path.join(dirpath, name)
            try:
                st = os.stat(full)
            except OSError:
                continue
            out[os.path.relpath(full, root)] = (st.st_mtime_ns, st.st_size)
            if len(out) > MAX_SCAN:
                return None
    return out


_PATHISH = re.compile(r"(?:[A-Za-z]:)?[\w.~\-/\\]+\.[A-Za-z0-9]{1,10}")


def mentioned(root: str, report: str) -> list[str]:
    """Files in root that the agent's report names, as paths relative to it.

    A run that was sent back to finish something often finds it already
    there and changes nothing; its report still says "notes/ideas.md", and
    that is the file the colleague checking it needs."""
    out: list[str] = []
    real_root = os.path.realpath(root)
    for token in _PATHISH.findall(report or "")[:100]:
        token = token.strip(".,;:()[]`'\"")
        full = os.path.realpath(token if os.path.isabs(token) else os.path.join(root, token))
        try:
            inside = os.path.commonpath([real_root, full]) == real_root
        except ValueError:  # another drive
            inside = False
        if inside and os.path.isfile(full):
            rel = os.path.relpath(full, real_root)
            if rel not in out:
                out.append(rel)
        if len(out) >= MAX_FILES:
            break
    return out


def produced(root: str, before: Optional[dict[str, tuple[int, int]]],
             report: str = "") -> tuple[list[dict[str, str]], list[str]]:
    """The text files created or changed since ``before`` (newest first),
    then those the report names, within the limits; and the names of the
    changed files that were not sent."""
    after = snapshot(root) if before is not None else None
    changed: list[str] = []
    if after is not None and before is not None:
        changed = [p for p, sig in after.items() if before.get(p) != sig]
        changed.sort(key=lambda p: after[p][0], reverse=True)
    for rel in mentioned(root, report):
        if rel not in changed:
            changed.append(rel)
    files: list[dict[str, str]] = []
    unshared: list[str] = []
    total = 0
    for rel in changed:
        name = rel.replace(os.sep, "/")
        try:
            size = os.path.getsize(os.path.join(root, rel))
        except OSError:
            continue
        if len(files) >= MAX_FILES or size > MAX_FILE_BYTES or total + size > MAX_TOTAL_BYTES:
            unshared.append(name)
            continue
        try:
            with open(os.path.join(root, rel), "rb") as fh:
                data = fh.read(MAX_FILE_BYTES + 1)
        except OSError:
            continue
        if b"\0" in data[:8192]:
            unshared.append(name)
            continue
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            unshared.append(name)
            continue
        files.append({"path": name, "content": text})
        total += len(data)
    return files, unshared[:50]


@dataclass
class Invocation:
    argv: list[str]
    stdin: Optional[str]
    parse: Callable[[str, "Parser"], None]


@dataclass
class Parser:
    """Accumulates what a CLI printed into progress events and a Result."""

    result: Result = field(default_factory=Result)
    events: list[dict[str, str]] = field(default_factory=list)
    text: list[str] = field(default_factory=list)
    lock: threading.Lock = field(default_factory=threading.Lock)

    def event(self, kind: str, text: str) -> None:
        text = (text or "").strip()
        if not text:
            return
        with self.lock:
            self.events.append({"kind": kind, "text": text[:600]})

    def drain(self) -> list[dict[str, str]]:
        with self.lock:
            out, self.events = self.events, []
        return out


# ----------------------------------------------------------- claude code ---

def claude_invocation(job: dict[str, Any], base: list[str]) -> Invocation:
    args = base + ["--print", "--output-format", "stream-json", "--verbose"]
    if job.get("session_id"):
        args += ["--resume", str(job["session_id"])]
    if job.get("autonomy") == "full":
        args += ["--dangerously-skip-permissions"]
    else:
        args += ["--permission-mode", "acceptEdits"]
    if job.get("model"):
        args += ["--model", str(job["model"])]
    args += [str(a) for a in (job.get("extra_args") or [])]
    return Invocation(args, str(job.get("prompt", "")), parse_claude_line)


def _tool_summary(name: str, inp: Any) -> str:
    if not isinstance(inp, dict):
        return name
    for key in ("command", "file_path", "path", "pattern", "url", "description"):
        if inp.get(key):
            return f"{name}: {str(inp[key])[:200]}"
    return name


def parse_claude_line(line: str, p: Parser) -> None:
    try:
        ev = json.loads(line)
    except ValueError:
        return
    kind = ev.get("type")
    if kind == "system" and ev.get("subtype") == "init":
        p.result.session_id = ev.get("session_id") or p.result.session_id
        p.result.model = ev.get("model") or p.result.model
    elif kind == "assistant":
        for block in (ev.get("message") or {}).get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                p.text.append(block["text"])
                p.event("text", block["text"])
            elif block.get("type") == "tool_use":
                p.event("tool", _tool_summary(block.get("name", "tool"), block.get("input")))
    elif kind == "result":
        r = p.result
        r.session_id = ev.get("session_id") or r.session_id
        r.answer = ev.get("result") or r.answer
        r.cost_usd = float(ev.get("total_cost_usd") or 0)
        r.is_error = bool(ev.get("is_error")) or ev.get("subtype") not in (None, "success")
        if r.is_error:
            r.error = str(ev.get("result") or ev.get("subtype") or "claude reported an error")
        usage = ev.get("usage") or {}
        model_usage = ev.get("modelUsage") or {}
        if model_usage:
            for m, u in model_usage.items():
                r.input_tokens += int(u.get("inputTokens", 0)) + int(u.get("cacheCreationInputTokens", 0))
                r.output_tokens += int(u.get("outputTokens", 0))
                r.cached_tokens += int(u.get("cacheReadInputTokens", 0))
                r.model = r.model or m
        else:
            r.input_tokens = int(usage.get("input_tokens", 0)) + int(usage.get("cache_creation_input_tokens", 0))
            r.output_tokens = int(usage.get("output_tokens", 0))
            r.cached_tokens = int(usage.get("cache_read_input_tokens", 0))


# ----------------------------------------------------------------- codex ---

def codex_invocation(job: dict[str, Any], base: list[str]) -> Invocation:
    args = base + ["exec", "--json", "--skip-git-repo-check"]
    if job.get("autonomy") == "full":
        args += ["--dangerously-bypass-approvals-and-sandbox"]
    else:
        args += ["-c", 'sandbox_mode="workspace-write"', "-c", "sandbox_workspace_write.network_access=true"]
    if job.get("model"):
        args += ["--model", str(job["model"])]
    args += [str(a) for a in (job.get("extra_args") or [])]
    if job.get("session_id"):
        args += ["resume", str(job["session_id"]), "-"]
    else:
        args += ["-"]
    return Invocation(args, str(job.get("prompt", "")), parse_codex_line)


def parse_codex_line(line: str, p: Parser) -> None:
    try:
        ev = json.loads(line)
    except ValueError:
        return
    kind = ev.get("type")
    if kind == "thread.started":
        p.result.session_id = ev.get("thread_id") or p.result.session_id
    elif kind == "item.completed":
        item = ev.get("item") or {}
        itype = item.get("type") or item.get("item_type")
        if itype == "agent_message":
            p.result.answer = item.get("text") or p.result.answer
            p.event("text", item.get("text", ""))
        elif itype in ("command_execution", "command"):
            p.event("tool", "ran: " + str(item.get("command", ""))[:200])
        elif itype in ("file_change", "patch"):
            p.event("tool", "changed files")
        elif itype == "reasoning" and item.get("text"):
            p.event("status", item["text"])
    elif kind == "turn.completed":
        u = ev.get("usage") or {}
        p.result.input_tokens += int(u.get("input_tokens", 0))
        p.result.cached_tokens += int(u.get("cached_input_tokens", 0))
        p.result.output_tokens += int(u.get("output_tokens", 0))
    elif kind in ("error", "turn.failed"):
        err = ev.get("message") or (ev.get("error") or {}).get("message") or "codex reported an error"
        p.result.is_error, p.result.error = True, str(err)


# ---------------------------------------------------------------- hermes ---

_HERMES_SESSION = re.compile(r"^session_id:\s*(\S+)", re.M)
_HERMES_TOKENS = re.compile(r"tokens?[:\s]+(\d+)\s*(?:input|in)\D+(\d+)\s*(?:output|out)", re.I)
_HERMES_COST = re.compile(r"(?:cost|spent)[:\s]*\$?([\d.]+)", re.I)


def hermes_invocation(job: dict[str, Any], base: list[str]) -> Invocation:
    args = base + ["chat", "-q", str(job.get("prompt", "")), "--source", "tool"]
    if job.get("model"):
        args += ["-m", str(job["model"])]
    if job.get("autonomy") == "full":
        args += ["--yolo"]
    if job.get("session_id"):
        args += ["--resume", str(job["session_id"])]
    args += [str(a) for a in (job.get("extra_args") or [])]
    return Invocation(args, None, parse_hermes_line)


def parse_hermes_line(line: str, p: Parser) -> None:
    m = _HERMES_SESSION.match(line.strip())
    if m:
        p.result.session_id = m.group(1)
        return
    t = _HERMES_TOKENS.search(line)
    if t:
        p.result.input_tokens, p.result.output_tokens = int(t.group(1)), int(t.group(2))
        return
    c = _HERMES_COST.search(line)
    if c and ("cost" in line.lower() or "spent" in line.lower()):
        try:
            p.result.cost_usd = float(c.group(1))
        except ValueError:
            pass
        return
    if line.strip():
        p.text.append(line.rstrip("\n"))
        p.event("text", line)


INVOCATIONS = {"claude_code": claude_invocation, "codex": codex_invocation, "hermes": hermes_invocation}


# ------------------------------------------------------------------ run ---

def run_agent(job: dict[str, Any], cwd: str, api_url: str,
              report: Callable[[list[dict[str, str]]], bool],
              timeout: float = 1800.0) -> tuple[str, Result]:
    """Run one agent job to the end.

    ``report`` is called with batches of progress events and returns True when
    the orchestrator wants the run stopped. Returns the device job state
    (done or failed) and the Result.
    """
    runtime = str(job.get("runtime", ""))
    base = resolve(runtime)
    if base is None:
        r = Result(is_error=True, error=f"{LABELS.get(runtime, runtime)} is not installed on this PC (no '{RUNTIMES.get(runtime, runtime)}' on PATH)")
        return "failed", r
    inv = INVOCATIONS[runtime](job, base)

    env = {k: v for k, v in os.environ.items() if k not in _STRIP_ENV}
    for k, v in (job.get("env") or {}).items():
        env[str(k)] = str(v)
    env["AGENTFLEET_API_URL"] = api_url

    before = snapshot(cwd)

    popen_kw: dict[str, Any] = {}
    if os.name == "nt":
        popen_kw["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP  # type: ignore[attr-defined]
    else:
        popen_kw["start_new_session"] = True
    try:
        proc = subprocess.Popen(
            inv.argv, cwd=cwd, env=env,
            stdin=subprocess.PIPE if inv.stdin is not None else subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", errors="replace", bufsize=1, **popen_kw,
        )
    except OSError as exc:
        return "failed", Result(is_error=True, error=f"could not start {inv.argv[0]}: {exc}")

    if inv.stdin is not None:
        try:
            assert proc.stdin is not None
            proc.stdin.write(inv.stdin)
            proc.stdin.close()
        except (BrokenPipeError, OSError):
            pass

    p = Parser()
    stderr_tail: list[str] = []

    def read_err() -> None:
        assert proc.stderr is not None
        for line in proc.stderr:
            stderr_tail.append(line)
            if len(stderr_tail) > 200:
                del stderr_tail[:100]

    t_err = threading.Thread(target=read_err, daemon=True)
    t_err.start()

    stop = threading.Event()
    stopped_by_server = threading.Event()

    def pump() -> None:
        # Reports every two seconds, and asks whether to stop.
        while not stop.wait(2.0):
            try:
                if report(p.drain()):
                    stopped_by_server.set()
                    _kill(proc)
                    return
            except Exception:  # noqa: BLE001 - a flaky link must not kill the run
                pass

    t_pump = threading.Thread(target=pump, daemon=True)
    t_pump.start()

    deadline = time.monotonic() + timeout
    timed_out = False

    def watchdog() -> None:
        nonlocal timed_out
        while proc.poll() is None:
            if time.monotonic() > deadline:
                timed_out = True
                _kill(proc)
                return
            time.sleep(1)

    threading.Thread(target=watchdog, daemon=True).start()

    assert proc.stdout is not None
    for line in proc.stdout:
        inv.parse(line, p)
    proc.wait()
    stop.set()
    t_err.join(timeout=2)
    for stream in (proc.stdout, proc.stderr):
        try:
            stream.close()
        except Exception:  # noqa: BLE001
            pass
    try:
        report(p.drain())
    except Exception:  # noqa: BLE001
        pass

    r = p.result
    r.exit_code = proc.returncode or 0
    if not r.answer and p.text:
        r.answer = "\n".join(p.text[-40:]).strip()
    if stopped_by_server.is_set():
        r.is_error, r.error = True, "stopped from the console"
    elif timed_out:
        r.is_error, r.error = True, f"timed out after {int(timeout)}s"
    elif r.exit_code != 0 and not r.is_error:
        r.is_error = True
        r.error = ("".join(stderr_tail[-20:]).strip() or f"exited with code {r.exit_code}")[:2000]
    elif r.is_error and not r.error:
        r.error = "".join(stderr_tail[-20:]).strip()[:2000]
    if not r.is_error:
        r.files, r.unshared = produced(cwd, before, r.answer)
    return ("failed" if r.is_error else "done"), r


def _kill(proc: subprocess.Popen) -> None:
    if proc.poll() is not None:
        return
    try:
        if os.name == "nt":
            # The whole tree: a CLI's own children (a dev server it started)
            # must not outlive the run.
            subprocess.run(["taskkill", "/T", "/F", "/PID", str(proc.pid)], capture_output=True, timeout=15)
        else:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except Exception:  # noqa: BLE001
        try:
            proc.kill()
        except Exception:  # noqa: BLE001
            pass
