"""The host's agent runtimes, against fake CLIs that print what the real ones do.

Each fake is a small Python script put on PATH through the
AGENTFLEET_<RUNTIME>_BIN override, so these run with no agent CLI installed.
"""

import json
import os
import sys
import tempfile
import textwrap
import time
import unittest
from pathlib import Path
from unittest import mock

from agentfleet import runtimes

FAKE_CLAUDE = r'''
import json, sys, os
prompt = sys.stdin.read()
args = sys.argv[1:]
open(os.path.join(os.getcwd(), "claude-args.json"), "w").write(json.dumps({"args": args, "prompt": prompt, "env_token": os.environ.get("AGENTFLEET_RUN_TOKEN"), "nested": os.environ.get("CLAUDECODE")}))
print(json.dumps({"type": "system", "subtype": "init", "session_id": "sess-new", "model": "claude-sonnet"}), flush=True)
print(json.dumps({"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Edit", "input": {"file_path": "app.js"}}]}}), flush=True)
print(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "Fixed the null check."}]}}), flush=True)
print(json.dumps({"type": "result", "subtype": "success", "is_error": False, "result": "Fixed the null check in app.js; tests pass.",
                  "session_id": "sess-new", "total_cost_usd": 0.31,
                  "modelUsage": {"claude-sonnet": {"inputTokens": 1000, "cacheCreationInputTokens": 200, "outputTokens": 90, "cacheReadInputTokens": 5000}}}), flush=True)
'''

FAKE_CLAUDE_SLOW = r'''
import json, sys, time
sys.stdin.read()
print(json.dumps({"type": "system", "subtype": "init", "session_id": "s"}), flush=True)
for i in range(600):
    print(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "working %d" % i}]}}), flush=True)
    time.sleep(0.1)
'''

FAKE_CLAUDE_LOGIN = r'''
import json, sys
sys.stdin.read()
print(json.dumps({"type": "result", "subtype": "success", "is_error": True, "result": "Invalid API key · Please run /login"}), flush=True)
sys.exit(1)
'''

FAKE_CODEX = r'''
import json, sys
prompt = sys.stdin.read()
print(json.dumps({"type": "thread.started", "thread_id": "th-9"}), flush=True)
print(json.dumps({"type": "item.completed", "item": {"type": "command_execution", "command": "npm test"}}), flush=True)
print(json.dumps({"type": "item.completed", "item": {"type": "agent_message", "text": "All 12 tests pass. " + str(len(prompt))}}), flush=True)
print(json.dumps({"type": "turn.completed", "usage": {"input_tokens": 3000, "cached_input_tokens": 1000, "output_tokens": 120}}), flush=True)
print("ARGS " + json.dumps(sys.argv[1:]), file=sys.stderr, flush=True)
'''

FAKE_HERMES = r'''
import sys
args = sys.argv[1:]
q = args[args.index("-q") + 1]
print("Looking into it: " + q[:20])
print("The vendor page lists three plans.")
print("session_id: hs-4")
print("tokens: 2100 input, 300 output")
print("cost: $0.02")
'''


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.work = self.tmp / "work"
        self.work.mkdir()
        self.env = mock.patch.dict(os.environ, {"CLAUDECODE": "1"})
        self.env.start()

    def tearDown(self):
        self.env.stop()

    def fake(self, runtime, source):
        script = self.tmp / f"fake_{runtime}.py"
        script.write_text(textwrap.dedent(source), encoding="utf-8")
        os.environ[f"AGENTFLEET_{runtime.upper()}_BIN"] = str(script)
        self.addCleanup(os.environ.pop, f"AGENTFLEET_{runtime.upper()}_BIN", None)

    def run_job(self, job, report=None, timeout=60):
        reports = []

        def rep(events):
            reports.extend(events)
            return report(events) if report else False

        state, result = runtimes.run_agent(job, str(self.work), "http://fleet.test", rep, timeout=timeout)
        return state, result, reports

    def test_claude_code_runs_resumes_and_reports(self):
        self.fake("claude_code", FAKE_CLAUDE)
        job = {"runtime": "claude_code", "prompt": "You are working on ticket T-7", "session_id": "sess-old",
               "autonomy": "full", "model": "sonnet", "env": {"AGENTFLEET_RUN_TOKEN": "afr_x"}}
        state, r, reports = self.run_job(job)
        self.assertEqual(state, "done", r.error)
        self.assertEqual(r.answer, "Fixed the null check in app.js; tests pass.")
        self.assertEqual((r.session_id, r.cost_usd, r.input_tokens, r.output_tokens, r.cached_tokens), ("sess-new", 0.31, 1200, 90, 5000))
        seen = json.loads((self.work / "claude-args.json").read_text())
        self.assertEqual(seen["prompt"], "You are working on ticket T-7")
        for flag in ("--print", "--output-format", "stream-json", "--verbose", "--resume", "sess-old", "--dangerously-skip-permissions", "--model", "sonnet"):
            self.assertIn(flag, seen["args"])
        self.assertEqual(seen["env_token"], "afr_x")
        self.assertIsNone(seen["nested"], "a parent Claude Code session's variables are stripped")
        self.assertTrue(any("Edit: app.js" in e["text"] for e in reports), reports)
        self.assertEqual([f["path"] for f in r.files], ["claude-args.json"], "what it wrote is sent to the fleet")
        self.assertIn('"files"', r.to_json())

    def test_only_changed_text_files_are_shared(self):
        (self.work / "old.txt").write_text("untouched", encoding="utf-8")
        (self.work / ".git").mkdir()
        before = runtimes.snapshot(str(self.work))
        (self.work / "notes").mkdir()
        (self.work / "notes" / "ideas.md").write_bytes(b"# Ideas\n")
        (self.work / "logo.png").write_bytes(b"\x89PNG\r\n\x1a\n\0\0\0")
        (self.work / ".git" / "HEAD").write_text("ref: x", encoding="utf-8")
        (self.work / "big.log").write_text("x" * (runtimes.MAX_FILE_BYTES + 1), encoding="utf-8")
        files, unshared = runtimes.produced(str(self.work), before)
        self.assertEqual(files, [{"path": "notes/ideas.md", "content": "# Ideas\n"}])
        self.assertEqual(sorted(unshared), ["big.log", "logo.png"])

    def test_files_the_report_names_are_shared_even_unchanged(self):
        (self.work / "notes").mkdir()
        (self.work / "notes" / "ideas.md").write_bytes(b"1. Falling Catch\n")
        outside = self.tmp / "secret.txt"
        outside.write_text("no", encoding="utf-8")
        before = runtimes.snapshot(str(self.work))
        report = f"The file `notes/ideas.md` is already there; see also {outside} and missing.txt."
        files, _ = runtimes.produced(str(self.work), before, report)
        self.assertEqual(files, [{"path": "notes/ideas.md", "content": "1. Falling Catch\n"}],
                         "a named file in the folder is sent; one outside it, or one that does not exist, is not")
        absolute = f"Created {self.work / 'notes' / 'ideas.md'}."
        files, _ = runtimes.produced(str(self.work), before, absolute)
        self.assertEqual([f["path"] for f in files], ["notes/ideas.md"], "an absolute path inside the folder counts")

    def test_edit_only_autonomy_does_not_skip_permissions(self):
        inv = runtimes.claude_invocation({"prompt": "x"}, ["claude"])
        self.assertIn("acceptEdits", inv.argv)
        self.assertNotIn("--dangerously-skip-permissions", inv.argv)
        cx = runtimes.codex_invocation({"prompt": "x"}, ["codex"])
        self.assertIn('sandbox_mode="workspace-write"', cx.argv)
        self.assertNotIn("--dangerously-bypass-approvals-and-sandbox", cx.argv)

    def test_the_console_can_stop_a_run(self):
        self.fake("claude_code", FAKE_CLAUDE_SLOW)
        calls = {"n": 0}

        def stop_on_second(_events):
            calls["n"] += 1
            return calls["n"] >= 2

        t0 = time.time()
        state, r, _ = self.run_job({"runtime": "claude_code", "prompt": "go"}, report=stop_on_second)
        self.assertEqual(state, "failed")
        self.assertIn("stopped from the console", r.error)
        self.assertLess(time.time() - t0, 30, "a stopped run ends promptly")

    def test_a_run_times_out(self):
        self.fake("claude_code", FAKE_CLAUDE_SLOW)
        state, r, _ = self.run_job({"runtime": "claude_code", "prompt": "go"}, timeout=2)
        self.assertEqual(state, "failed")
        self.assertIn("timed out", r.error)

    def test_a_cli_that_is_not_signed_in_says_so(self):
        self.fake("claude_code", FAKE_CLAUDE_LOGIN)
        state, r, _ = self.run_job({"runtime": "claude_code", "prompt": "go"})
        self.assertEqual(state, "failed")
        self.assertIn("Please run /login", r.error)

    def test_codex(self):
        self.fake("codex", FAKE_CODEX)
        state, r, reports = self.run_job({"runtime": "codex", "prompt": "fix it", "session_id": "th-1"})
        self.assertEqual(state, "done", r.error)
        self.assertTrue(r.answer.startswith("All 12 tests pass."), r.answer)
        self.assertEqual((r.session_id, r.input_tokens, r.cached_tokens, r.output_tokens), ("th-9", 3000, 1000, 120))
        self.assertTrue(any("ran: npm test" in e["text"] for e in reports))
        inv = runtimes.codex_invocation({"prompt": "x", "session_id": "th-1"}, ["codex"])
        self.assertEqual(inv.argv[-3:], ["resume", "th-1", "-"])

    def test_hermes(self):
        self.fake("hermes", FAKE_HERMES)
        state, r, _ = self.run_job({"runtime": "hermes", "prompt": "compare the vendor plans", "autonomy": "full"})
        self.assertEqual(state, "done", r.error)
        self.assertIn("three plans", r.answer)
        self.assertEqual((r.session_id, r.input_tokens, r.output_tokens, r.cost_usd), ("hs-4", 2100, 300, 0.02))
        inv = runtimes.hermes_invocation({"prompt": "p", "autonomy": "full", "session_id": "hs-1"}, ["hermes"])
        for flag in ("chat", "-q", "p", "--source", "tool", "--yolo", "--resume", "hs-1"):
            self.assertIn(flag, inv.argv)

    def test_a_missing_cli_is_reported_not_raised(self):
        with mock.patch.object(runtimes.shutil, "which", lambda _name: None):
            state, r, _ = self.run_job({"runtime": "codex", "prompt": "x"})
        self.assertEqual(state, "failed")
        self.assertIn("not installed", r.error)

    def test_detect_finds_overridden_binaries(self):
        self.fake("hermes", FAKE_HERMES)
        with mock.patch.object(runtimes.shutil, "which", lambda _name: None):
            self.assertEqual(runtimes.detect(), ["hermes"])


if __name__ == "__main__":
    unittest.main()
