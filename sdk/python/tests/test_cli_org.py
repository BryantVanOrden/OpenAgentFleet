"""fleetctl's org, agent, device and fleet-template commands, against a fake client."""

import io
import json
import os
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest import mock

from agentfleet import cli
from agentfleet.models import InstanceInfo

ROOT = os.path.join(tempfile.gettempdir(), "af-work")


class FakeBot:
    def __init__(self, id, name, kind="desktop"):
        self.id, self.name = id, name
        self.info = InstanceInfo.from_dict({"id": id, "name": name, "kind": kind, "state": "running"})
        self.archetype_id, self.tier, self.state = None, "standard", "running"


class FakeClient:
    def __init__(self):
        self.calls = []
        self.bots = [FakeBot("b1", "Builder"), FakeBot("c1", "Claude", "claude_code")]
        self.devs = [{"id": "dev-123456", "name": "Dev PC", "kind": "pc", "online": True,
                      "runtimes": ["claude_code"], "roots": [ROOT]}]

    def list_bots(self):
        return self.bots

    def devices(self):
        return self.devs

    def add_external_agent(self, name, kind, connection, token=None, **fields):
        self.calls.append(("add", name, kind, connection, token, fields))
        return {"id": "new", "name": name}

    def set_profile(self, iid, **fields):
        self.calls.append(("profile", iid, fields))
        return {"name": "Claude"}

    def _get(self, path):
        self.calls.append(("get", path))
        return {"connection": {"device_id": "dev-123456", "cwd": ROOT, "autonomy": "edits"}}

    def export_fleet(self):
        return {"version": 1, "agents": [{"name": "Builder", "kind": "desktop"}]}

    def import_fleet(self, tpl, dry_run=False, rename=False):
        self.calls.append(("import", tpl, dry_run, rename))
        return {"created": ["Builder 2"] if rename else [], "skipped": [] if rename else ["Builder"], "renamed": [], "notes": []}


def run(argv, client):
    with mock.patch.object(cli, "_get_client", lambda _a: client), \
            mock.patch("sys.argv", ["fleetctl"] + argv):
        out = io.StringIO()
        with redirect_stdout(out):
            try:
                cli.main()
            except SystemExit as e:
                if e.code not in (None, 0):
                    return out.getvalue() + str(e.code)
        return out.getvalue()


class CliOrgTests(unittest.TestCase):
    def test_instance_info_reads_org_fields(self):
        i = InstanceInfo.from_dict({"id": "x", "name": "C", "kind": "codex", "reports_to": "b1", "title": "Dev",
                                    "budget_month_usd": 20, "connection": {"cwd": "/w"}})
        self.assertTrue(i.external)
        self.assertEqual((i.reports_to, i.title, i.budget_month_usd, i.connection["cwd"]), ("b1", "Dev", 20.0, "/w"))
        self.assertFalse(InstanceInfo.from_dict({"id": "d", "name": "D"}).external, "an older server's instance is a desktop")

    def test_add_a_claude_code_agent_on_the_only_pc(self):
        c = FakeClient()
        out = run(["agent", "add", "Reviewer", "--kind", "claude_code", "--model", "haiku",
                   "--reports-to", "Builder", "--title", "Staff engineer"], c)
        self.assertIn("Reviewer added", out)
        _, name, kind, conn, token, fields = c.calls[0]
        self.assertEqual((name, kind), ("Reviewer", "claude_code"))
        self.assertEqual(conn, {"device_id": "dev-123456", "cwd": ROOT, "autonomy": "edits", "model": "haiku"})
        self.assertEqual(fields, {"title": "Staff engineer", "reports_to": "b1"})
        self.assertIsNone(token)

    def test_a_folder_outside_the_pcs_roots_is_refused_here(self):
        c = FakeClient()
        out = run(["agent", "add", "X", "--kind", "claude_code", "--folder", os.path.join(tempfile.gettempdir(), "elsewhere")], c)
        self.assertIn("must be under one of Dev PC's roots", out)
        self.assertEqual(c.calls, [])

    def test_a_pc_without_the_cli_is_refused(self):
        c = FakeClient()
        out = run(["agent", "add", "X", "--kind", "codex"], c)
        self.assertIn("has no codex CLI", out)

    def test_webhook_needs_a_url_and_reads_its_token_from_the_environment(self):
        c = FakeClient()
        self.assertIn("needs --url", run(["agent", "add", "Hook", "--kind", "webhook"], c))
        with mock.patch.dict(os.environ, {"HOOK_TOKEN": "s3cret"}):
            run(["agent", "add", "Hook", "--kind", "webhook", "--url", "https://hooks.example/run", "--token-env", "HOOK_TOKEN"], c)
        _, _, _, conn, token, _ = c.calls[-1]
        self.assertEqual((conn, token), ({"url": "https://hooks.example/run"}, "s3cret"))

    def test_set_moves_an_agent_to_you_and_merges_its_connection(self):
        c = FakeClient()
        run(["agent", "set", "Claude", "--reports-to", "you", "--model", "sonnet"], c)
        _, iid, fields = c.calls[-1]
        self.assertEqual(iid, "c1")
        self.assertEqual(fields["reports_to"], "")
        self.assertEqual(fields["connection"], {"device_id": "dev-123456", "cwd": ROOT, "autonomy": "edits", "model": "sonnet"})

    def test_fleet_export_and_import(self):
        c = FakeClient()
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "fleet.json")
            self.assertIn("1 agents written", run(["fleet", "export", "-o", path], c))
            with open(path, encoding="utf-8") as fh:
                self.assertEqual(json.load(fh)["version"], 1)
            out = run(["fleet", "import", path, "--dry-run"], c)
            self.assertIn("would create: nobody", out)
            self.assertIn("skipped (name taken", out)
            self.assertEqual(c.calls[-1][2:], (True, False))
            self.assertIn("created: Builder 2", run(["fleet", "import", path, "--rename"], c))

    def test_list_shows_the_kind(self):
        out = run(["list"], FakeClient())
        self.assertIn("claude_code", out)
        self.assertIn("KIND", out)

    def test_devices(self):
        out = run(["devices"], FakeClient())
        self.assertIn("Dev PC", out)
        self.assertIn("Claude Code", out)


if __name__ == "__main__":
    unittest.main()
