"""Archetype packaging.

The README said: "The SDK writes an .agentfleet.json manifest (not
.agentfleet.yaml), with tools and recorded skills left empty. `hub import` reads
the file and prints a summary; it does not create anything."

These pin the parts that are testable without a fleet: that a `.yaml` file is
actually YAML, that it round-trips, that a system prompt full of colons and
newlines survives it, and that `hub import` posts the manifest somewhere instead
of printing and returning.
"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from agentfleet.client import FleetClient  # noqa: E402
from agentfleet.hub import ArchetypeManifest, dump_yaml, load_yaml  # noqa: E402


def sample(**overrides) -> ArchetypeManifest:
    data = dict(
        version="1.0.0",
        id="solana_auditor",
        name="Solana Smart Contract Auditor",
        tagline="Audits Anchor rust contracts",
        category="Web3 & Crypto",
        recommended_tier="developer-heavy",
        vcpu=8.0,
        memory_mb=16384,
        disk_gb=40,
        gpu=False,
        preinstalled_tools=["solana-cli", "anchor-cli", "cargo"],
        system_prompt="Audit smart contracts for reentrancy.",
        default_environment={"RUST_LOG": "info"},
        mcp_servers=[],
        recorded_skills=[],
    )
    data.update(overrides)
    return ArchetypeManifest(**data)


class TestManifestFormat(unittest.TestCase):
    def test_yaml_extension_produces_yaml_not_json(self):
        manifest = sample()
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "solana_auditor.agentfleet.yaml"
            manifest.save(path)
            text = path.read_text(encoding="utf-8")

        # save() used to json.dump() whatever the extension said, so the format
        # this module is named after was never actually produced.
        self.assertFalse(
            text.lstrip().startswith("{"),
            f"a .yaml file still contains JSON:\n{text[:200]}",
        )
        self.assertIn("id: ", text)
        self.assertIn("recommended_tier: ", text)

    def test_json_extension_still_produces_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "x.agentfleet.json"
            sample().save(path)
            # Anything that was exporting .json keeps working unchanged.
            data = json.loads(path.read_text(encoding="utf-8"))
        self.assertEqual(data["id"], "solana_auditor")

    def test_yaml_round_trips(self):
        original = sample(
            preinstalled_repos=["https://github.com/acme/contracts"],
            default_voice="shadow",
            default_shell_access=True,
        )
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.agentfleet.yaml"
            original.save(path)
            loaded = ArchetypeManifest.load(path)

        self.assertEqual(loaded.id, original.id)
        self.assertEqual(loaded.name, original.name)
        self.assertEqual(loaded.vcpu, 8.0)
        self.assertEqual(loaded.memory_mb, 16384)
        self.assertIs(loaded.gpu, False)
        self.assertIs(loaded.default_shell_access, True)
        self.assertEqual(loaded.preinstalled_tools, original.preinstalled_tools)
        self.assertEqual(loaded.default_environment, {"RUST_LOG": "info"})
        self.assertEqual(loaded.preinstalled_repos, original.preinstalled_repos)

    def test_a_multiline_prompt_survives_yaml(self):
        # A system prompt runs to paragraphs and is full of colons, hashes and
        # newlines -- every one of which changes the meaning of an unquoted YAML
        # scalar, and any of which would corrupt the prompt on the way back.
        prompt = (
            "You audit contracts.\n"
            "Rules:\n"
            "  - never approve unchecked math # not even once\n"
            'Say "APPROVED: yes" when done.\n'
            "Ratio 3:1, cost $5.00 — 100% required."
        )
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "b.agentfleet.yaml"
            sample(system_prompt=prompt).save(path)
            loaded = ArchetypeManifest.load(path)
        self.assertEqual(loaded.system_prompt, prompt)

    def test_nested_structures_survive(self):
        skills = [
            {"id": "sk-1", "name": "Deploy", "steps": [{"action": "click", "target": "Build"}]},
        ]
        servers = [{"name": "github", "transport": "stdio", "command": "npx", "env_keys": ["TOKEN"]}]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "c.agentfleet.yaml"
            sample(recorded_skills=skills, mcp_servers=servers).save(path)
            loaded = ArchetypeManifest.load(path)

        # These were hardcoded to [] by the exporter, so nothing ever exercised
        # them going through the file.
        self.assertEqual(loaded.recorded_skills, skills)
        self.assertEqual(loaded.mcp_servers, servers)

    def test_json_is_accepted_from_a_yaml_named_file(self):
        # Earlier versions of this tool wrote JSON into .yaml files. Refusing
        # them would reject manifests it produced itself.
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "legacy.agentfleet.yaml"
            path.write_text(json.dumps(sample().to_dict()), encoding="utf-8")
            loaded = ArchetypeManifest.load(path)
        self.assertEqual(loaded.id, "solana_auditor")

    def test_a_manifest_with_no_id_is_rejected(self):
        with self.assertRaises(ValueError):
            ArchetypeManifest.from_dict({"name": "nameless"})
        with self.assertRaises(ValueError):
            ArchetypeManifest.from_dict({"id": "x"})

    def test_empty_collections_round_trip_as_collections(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "d.agentfleet.yaml"
            sample(preinstalled_tools=[], default_environment={}).save(path)
            loaded = ArchetypeManifest.load(path)
        # [] and {}, not None: a caller iterating these should not have to
        # None-check them.
        self.assertEqual(loaded.preinstalled_tools, [])
        self.assertEqual(loaded.default_environment, {})


class TestYamlSubset(unittest.TestCase):
    def test_dump_and_load_are_inverses(self):
        data = {
            "s": "text",
            "i": 42,
            "f": 1.5,
            "t": True,
            "f2": False,
            "list": ["a", "b"],
            "empty_list": [],
            "map": {"k": "v", "n": 3},
            "empty_map": {},
            "nested": [{"a": 1}],
        }
        self.assertEqual(load_yaml(dump_yaml(data)), data)

    def test_comments_and_blank_lines_are_ignored(self):
        text = "# a comment\n\nid: x\n\n# another\nname: y\n"
        self.assertEqual(load_yaml(text), {"id": "x", "name": "y"})


class TestHubImportActuallyInstalls(unittest.TestCase):
    """`hub import` used to print three lines and create nothing."""

    def _args(self, path: Path, **kw):
        import argparse

        ns = argparse.Namespace(
            file=str(path),
            url="http://localhost:8080",
            token="t",
            overwrite=False,
            create_instance=False,
            instance_name=None,
            dry_run=False,
        )
        for k, v in kw.items():
            setattr(ns, k, v)
        return ns

    @patch.object(FleetClient, "_post")
    def test_import_posts_the_manifest(self, mock_post):
        from agentfleet.cli import cmd_hub_import

        mock_post.return_value = {
            "archetype": "solana_auditor",
            "skills_created": ["Deploy"],
            "skills_skipped": [],
            "mcp_registered": [],
            "mcp_failed": [],
        }
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.agentfleet.yaml"
            sample().save(path)
            cmd_hub_import(self._args(path))

        # The assertion the old implementation could never pass: something was
        # actually sent to the fleet.
        mock_post.assert_called_once()
        endpoint, body = mock_post.call_args[0]
        self.assertEqual(endpoint, "/api/archetypes/import")
        self.assertEqual(body["manifest"]["id"], "solana_auditor")
        self.assertFalse(body["overwrite"])
        self.assertFalse(body["create_instance"])

    @patch.object(FleetClient, "_post")
    def test_dry_run_installs_nothing(self, mock_post):
        from agentfleet.cli import cmd_hub_import

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.agentfleet.yaml"
            sample().save(path)
            cmd_hub_import(self._args(path, dry_run=True))
        mock_post.assert_not_called()

    @patch.object(FleetClient, "_post")
    def test_flags_are_passed_through(self, mock_post):
        from agentfleet.cli import cmd_hub_import

        mock_post.return_value = {"archetype": "x", "skills_created": [], "skills_skipped": []}
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.agentfleet.yaml"
            sample().save(path)
            cmd_hub_import(
                self._args(path, overwrite=True, create_instance=True, instance_name="Auditor 1")
            )
        _, body = mock_post.call_args[0]
        self.assertTrue(body["overwrite"])
        self.assertTrue(body["create_instance"])
        self.assertEqual(body["instance_name"], "Auditor 1")


class TestHubExportUsesTheEndpoint(unittest.TestCase):
    def _args(self, out: Path, **kw):
        import argparse

        ns = argparse.Namespace(
            archetype="solana_auditor",
            output=str(out),
            skills=None,
            url="http://localhost:8080",
            token="t",
        )
        for k, v in kw.items():
            setattr(ns, k, v)
        return ns

    @patch.object(FleetClient, "_get")
    def test_export_asks_the_server_and_keeps_what_it_returns(self, mock_get):
        from agentfleet.cli import cmd_hub_export

        # The server is the only thing that knows the fleet's skills and MCP
        # registrations. The exporter used to build the manifest locally from
        # the template catalogue and hardcode both to [].
        mock_get.return_value = {
            **sample().to_dict(),
            "recorded_skills": [{"id": "sk-1", "name": "Deploy", "steps": []}],
            "mcp_servers": [{"name": "github", "transport": "stdio", "env_keys": ["TOKEN"]}],
        }

        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "x.agentfleet.yaml"
            cmd_hub_export(self._args(out))
            written = ArchetypeManifest.load(out)

        mock_get.assert_called_once_with("/api/archetypes/solana_auditor/export")
        self.assertEqual(len(written.recorded_skills), 1)
        self.assertEqual(len(written.mcp_servers), 1)

    @patch.object(FleetClient, "_get")
    def test_skill_filter_reaches_the_query_string(self, mock_get):
        from agentfleet.cli import cmd_hub_export

        mock_get.return_value = sample().to_dict()
        with tempfile.TemporaryDirectory() as tmp:
            cmd_hub_export(self._args(Path(tmp) / "x.yaml", skills="sk-1,sk-2"))
        mock_get.assert_called_once_with("/api/archetypes/solana_auditor/export?skills=sk-1,sk-2")


if __name__ == "__main__":
    unittest.main()
