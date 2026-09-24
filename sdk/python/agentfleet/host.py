"""`fleetctl host`: let Oaf -- and your external agents -- act on this machine.

Registers this PC as a device, then polls the orchestrator for jobs -- run a
command, read or write a file, list a folder, search -- and executes them
here, inside the folders you exposed. Anything that changes state (a shell
command, a write) asks you first in this terminal, the way Claude Code asks
before it runs something; ``--yes`` skips the asking for a session you trust.

It also runs external agents. An agent the console created as Claude Code,
Codex or Hermes on this PC arrives as an ``agent_run`` job: the host starts the
CLI in the agent's folder, streams what it does back, and posts its answer.
Each run asks here first unless ``--yes``.

Nothing here is reachable from the network: the machine polls out, the
orchestrator never connects in. Stop the process and Oaf loses the machine.
"""

from __future__ import annotations

import json
import os
import platform
import re
import shutil
import subprocess
import sys
import threading
import time
import webbrowser
from pathlib import Path
from typing import Any, Optional

from agentfleet import runtimes as _runtimes
from agentfleet.client import FleetClient
from agentfleet.exceptions import FleetApiError

MAX_OUTPUT = 60_000  # characters of command output kept per job
POLL_WAIT = 15  # seconds a poll waits for a job; also the heartbeat cadence
DEVICE_FILE = Path.home() / ".agentfleet" / "device.json"


class RootJail:
    """Resolves paths and refuses anything outside the exposed folders."""

    def __init__(self, roots: list[str]) -> None:
        self.roots = [Path(r).expanduser().resolve() for r in roots]

    def resolve(self, cwd: str, rel: str) -> Path:
        base = Path(cwd).expanduser().resolve() if cwd else (self.roots[0] if self.roots else Path.cwd().resolve())
        p = Path(rel).expanduser()
        target = (p if p.is_absolute() else base / p).resolve()
        for root in self.roots:
            try:
                target.relative_to(root)
                return target
            except ValueError:
                continue
        raise PermissionError(f"{target} is outside the folders this device exposes ({', '.join(map(str, self.roots))})")

    def cwd(self, cwd: str) -> Path:
        if not cwd:
            if not self.roots:
                return Path.cwd()
            return self.roots[0]
        return self.resolve("", cwd)


class Host:
    def __init__(self, client: FleetClient, name: str, roots: list[str], auto_approve: bool, quiet: bool = False) -> None:
        self.client = client
        self.name = name
        self.jail = RootJail(roots)
        self.auto_approve = auto_approve
        self.quiet = quiet
        self.device_id: Optional[str] = None
        self.runtimes: list[str] = _runtimes.detect()
        # One question at a time in the terminal, however many agents ask.
        self._ask = threading.Lock()

    # ------------------------------------------------------------ register ---

    def register(self) -> dict[str, Any]:
        saved: dict[str, Any] = {}
        if DEVICE_FILE.exists():
            try:
                saved = json.loads(DEVICE_FILE.read_text(encoding="utf-8"))
            except Exception:
                saved = {}
        body = {
            "id": saved.get("id", ""),
            "name": self.name,
            "kind": "pc",
            "platform": f"{platform.system()} {platform.release()}",
            "roots": [str(r) for r in self.jail.roots],
            "auto_approve": self.auto_approve,
            "runtimes": self.runtimes,
        }
        dev = None
        for attempt in range(5):
            try:
                dev = self.client._post("/api/oaf/devices", body)
                break
            except (FleetApiError, OSError) as exc:
                # A reset or timed-out socket: the orchestrator restarted, or
                # a proxy blinked. Try again before giving up on the device.
                if attempt == 4:
                    raise
                print(f"[host] registration failed ({exc}); retrying", file=sys.stderr)
                time.sleep(2 * (attempt + 1))
        self.device_id = dev["id"]
        DEVICE_FILE.parent.mkdir(parents=True, exist_ok=True)
        DEVICE_FILE.write_text(json.dumps({"id": self.device_id, "name": self.name}), encoding="utf-8")
        return dev

    # ----------------------------------------------------------------- loop ---

    def run_forever(self) -> None:
        # The poll holds a request open for POLL_WAIT seconds; the client's
        # timeout has to be comfortably longer or every quiet minute is a
        # "timed out" crash, which is exactly what took the first host down.
        self.client.timeout = max(self.client.timeout, POLL_WAIT + 20)
        dev = self.register()
        roots = ", ".join(str(r) for r in self.jail.roots) or "(no folders exposed)"
        print(f"[host] {dev['name']} registered as {dev['id'][:8]} — folders: {roots}", flush=True)
        print("[host] " + ("auto-approving shell and writes (--yes)" if self.auto_approve else "shell commands and writes will ask here first"), flush=True)
        if self.runtimes:
            names = ", ".join(_runtimes.LABELS.get(r, r) for r in self.runtimes)
            print(f"[host] agent CLIs found: {names} -- agents can run here", flush=True)
        else:
            print("[host] no agent CLIs found (claude, codex, hermes); external agents cannot run here", flush=True)
        print("[host] waiting for Oaf. Ctrl+C to disconnect.", flush=True)
        backoff = 2
        while True:
            try:
                jobs = self.client._request("GET", f"/api/oaf/devices/{self.device_id}/jobs?wait={POLL_WAIT}")
                backoff = 2
            except KeyboardInterrupt:
                raise
            except Exception as exc:  # noqa: BLE001 - a flaky link must never take the device offline
                print(f"[host] {type(exc).__name__}: {exc}; retrying in {backoff}s", file=sys.stderr, flush=True)
                time.sleep(backoff)
                backoff = min(backoff * 2, 30)
                continue
            for job in jobs or []:
                if job.get("kind") == "agent_run":
                    # Agent runs take minutes; the loop keeps serving Oaf.
                    threading.Thread(target=self.handle_agent, args=(job,), daemon=True).start()
                else:
                    self.handle(job)

    def handle(self, job: dict[str, Any]) -> None:
        kind = job.get("kind", "")
        args = job.get("args") or {}
        started = time.time()
        try:
            state, result = self.execute(kind, args)
            error = ""
        except PermissionError as exc:
            state, result, error = "denied", "", str(exc)
        except Exception as exc:  # noqa: BLE001 - every failure is reported, none crashes the loop
            state, result, error = "failed", "", f"{type(exc).__name__}: {exc}"
        if not self.quiet:
            took = time.time() - started
            print(f"[host] {kind} → {state} ({took:.1f}s)" + (f": {error}" if error else ""), flush=True)
        for attempt in range(3):
            try:
                self.client._post(f"/api/oaf/devices/{self.device_id}/jobs/{job['id']}/result",
                                  {"state": state, "result": result[:MAX_OUTPUT], "error": error[:2000]})
                break
            except (FleetApiError, OSError) as exc:
                if attempt == 2:
                    print(f"[host] could not report job {job['id'][:8]}: {exc}", file=sys.stderr)
                else:
                    time.sleep(2)

    # ------------------------------------------------------------ agent runs ---

    def handle_agent(self, job: dict[str, Any]) -> None:
        args = job.get("args") or {}
        runtime = str(args.get("runtime", ""))
        label = _runtimes.LABELS.get(runtime, runtime)
        agent = str(args.get("agent", "an agent"))
        ticket = str(args.get("ticket", "a ticket"))
        started = time.time()
        try:
            cwd = self.jail.cwd(str(args.get("cwd", "")))
        except PermissionError as exc:
            self._finish(job, "denied", "", str(exc))
            return
        autonomy = "full autonomy (it can run anything in that folder)" if args.get("autonomy") == "full" else "edit-only autonomy"
        if not self.approve(f"let {label} ({agent}) work on {ticket} in {cwd} with {autonomy}", who=agent):
            self._finish(job, "denied", "", "declined in the host terminal")
            return
        for name in self._write_incoming(cwd, args.get("files") or []):
            if not self.quiet:
                print(f"[host] {agent}: {name} updated by a colleague, written into the folder", flush=True)
        if not self.quiet:
            print(f"[host] {agent}: {label} started on {ticket} in {cwd}", flush=True)

        def report(events: list[dict[str, str]]) -> bool:
            if not self.quiet:
                for ev in events:
                    if ev.get("kind") == "tool":
                        print(f"[host] {agent}: {ev['text'][:160]}", flush=True)
            res = self.client._post(f"/api/oaf/devices/{self.device_id}/jobs/{job['id']}/progress", {"events": events[:50]})
            return bool((res or {}).get("cancel"))

        timeout = float(args.get("timeout_sec") or 1800)
        state, result = _runtimes.run_agent(args, str(cwd), self.client.base_url, report, timeout=timeout)
        if not self.quiet:
            took = time.time() - started
            cost = f", ${result.cost_usd:.2f}" if result.cost_usd else ""
            print(f"[host] {agent}: {label} {state} after {took:.0f}s{cost}" + (f": {result.error[:200]}" if result.error else ""), flush=True)
        self._finish(job, state, result.to_json(), result.error)

    @staticmethod
    def _write_incoming(cwd: Path, files: list[dict[str, Any]]) -> list[str]:
        """Writes the newer versions colleagues published of files this agent
        shared, inside its folder and nowhere else."""
        written: list[str] = []
        root = Path(cwd).resolve()
        for f in files:
            rel = str(f.get("path") or "").replace("\\", "/").strip("/")
            if not rel or any(part == ".." for part in rel.split("/")):
                continue
            target = (root / rel).resolve()
            if root != target and root not in target.parents:
                continue
            try:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(str(f.get("content") or ""), encoding="utf-8", newline="")
                written.append(rel)
            except OSError as exc:
                print(f"[host] could not write {rel}: {exc}", file=sys.stderr)
        return written

    def _finish(self, job: dict[str, Any], state: str, result: str, error: str) -> None:
        for attempt in range(5):
            try:
                self.client._post(f"/api/oaf/devices/{self.device_id}/jobs/{job['id']}/result",
                                  {"state": state, "result": result[:190_000], "error": (error or "")[:2000]})
                return
            except (FleetApiError, OSError) as exc:
                if attempt == 4:
                    print(f"[host] could not report job {job['id'][:8]}: {exc}", file=sys.stderr)
                else:
                    time.sleep(2 * (attempt + 1))

    # -------------------------------------------------------------- approval ---

    def approve(self, what: str, who: str = "Oaf") -> bool:
        if self.auto_approve:
            return True
        if not sys.stdin.isatty():
            print(f"[host] no terminal to ask on; refusing: {what}", file=sys.stderr)
            return False
        with self._ask:
            try:
                answer = input(f"\n[host] {who} wants to {what}\n       allow? [y/N] ").strip().lower()
            except EOFError:
                return False
        return answer in ("y", "yes")

    # ---------------------------------------------------------------- tools ---

    def execute(self, kind: str, args: dict[str, Any]) -> tuple[str, str]:
        cwd = self.jail.cwd(str(args.get("cwd", "")))
        if kind == "shell":
            cmd = str(args.get("command", "")).strip()
            if not cmd:
                raise ValueError("shell needs a command")
            if not self.approve(f"run in {cwd}:\n         {cmd}"):
                return "denied", ""
            timeout = float(args.get("timeout_sec") or 120)
            shell = os.environ.get("COMSPEC", "cmd.exe") if os.name == "nt" else "/bin/sh"
            flag = "/c" if os.name == "nt" else "-c"
            try:
                proc = subprocess.run([shell, flag, cmd], cwd=str(cwd), capture_output=True, text=True,
                                      errors="replace", timeout=timeout)
            except subprocess.TimeoutExpired:
                return "failed", f"timed out after {timeout:.0f}s"
            out = proc.stdout
            if proc.stderr:
                out += ("\n[stderr]\n" if out else "[stderr]\n") + proc.stderr
            out = _tail(out, MAX_OUTPUT)
            if proc.returncode != 0:
                return "failed", f"exit {proc.returncode}\n{out}"
            return "done", out or "(no output)"

        if kind == "read_file":
            p = self.jail.resolve(str(cwd), str(args.get("path", "")))
            if p.is_dir():
                raise IsADirectoryError(f"{p} is a folder; use list_dir")
            data = p.read_bytes()
            if len(data) > 400_000:
                return "done", data[:400_000].decode("utf-8", errors="replace") + f"\n…[{len(data) - 400_000} more bytes]"
            return "done", data.decode("utf-8", errors="replace")

        if kind == "write_file":
            p = self.jail.resolve(str(cwd), str(args.get("path", "")))
            content = str(args.get("content", ""))
            verb = "overwrite" if p.exists() else "create"
            if not self.approve(f"{verb} {p} ({len(content)} chars)"):
                return "denied", ""
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(content, encoding="utf-8")
            return "done", f"wrote {len(content)} chars to {p}"

        if kind == "list_dir":
            p = self.jail.resolve(str(cwd), str(args.get("path", ".")))
            if not p.is_dir():
                raise NotADirectoryError(str(p))
            lines = []
            for child in sorted(p.iterdir(), key=lambda c: (not c.is_dir(), c.name.lower()))[:500]:
                if child.name in (".git", "node_modules", "__pycache__", ".venv", "venv"):
                    lines.append(f"{child.name}/  (skipped)")
                    continue
                lines.append(f"{child.name}/" if child.is_dir() else f"{child.name}  {child.stat().st_size}")
            return "done", "\n".join(lines) or "(empty)"

        if kind == "search":
            pattern = str(args.get("pattern", ""))
            if not pattern:
                raise ValueError("search needs a pattern")
            root = self.jail.resolve(str(cwd), str(args.get("path", ".")))
            rx = re.compile(pattern)
            hits: list[str] = []
            skip = {".git", "node_modules", "__pycache__", ".venv", "venv", "dist", "build"}
            for dirpath, dirnames, filenames in os.walk(root):
                dirnames[:] = [d for d in dirnames if d not in skip]
                for fn in filenames:
                    fp = Path(dirpath) / fn
                    try:
                        if fp.stat().st_size > 2_000_000:
                            continue
                        with fp.open("r", encoding="utf-8", errors="ignore") as fh:
                            for n, line in enumerate(fh, 1):
                                if rx.search(line):
                                    hits.append(f"{fp.relative_to(root)}:{n}: {line.rstrip()[:200]}")
                                    if len(hits) >= 200:
                                        return "done", "\n".join(hits) + "\n…(capped at 200 hits)"
                    except OSError:
                        continue
            return "done", "\n".join(hits) or "(no matches)"

        if kind == "open_url":
            url = str(args.get("url", ""))
            if not url.startswith(("http://", "https://")):
                raise ValueError("open_url needs an http(s) url")
            if not self.approve(f"open {url} in your browser"):
                return "denied", ""
            webbrowser.open(url)
            return "done", f"opened {url}"

        if kind == "notify":
            text = str(args.get("text", ""))
            print(f"\n[host] 🔔 {text}\n")
            return "done", "shown in the host terminal"

        if kind == "clipboard":
            return "failed", "clipboard is a phone tool"

        if kind == "screenshot":
            return "failed", "screenshots are a phone tool (a PC's screen is not exposed by fleetctl host)"

        raise ValueError(f"unknown job kind {kind}")


def _tail(s: str, n: int) -> str:
    if len(s) <= n:
        return s
    return f"…[{len(s) - n} chars omitted]…\n" + s[-n:]


def run(client: FleetClient, name: str, roots: list[str], auto_approve: bool, quiet: bool) -> None:
    if not roots:
        roots = [os.getcwd()]
    missing = [r for r in roots if not Path(r).expanduser().exists()]
    if missing:
        raise SystemExit(f"folder not found: {', '.join(missing)}")
    if shutil.which("git") is None and not quiet:
        print("[host] note: git is not on PATH; Oaf's shell commands that need it will fail")
    Host(client, name, roots, auto_approve, quiet).run_forever()
