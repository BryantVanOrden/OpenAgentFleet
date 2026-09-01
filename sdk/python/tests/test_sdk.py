import io
import json
import os
import unittest
import urllib.error
from unittest.mock import MagicMock, patch

import agentfleet
from agentfleet.client import FleetClient
from agentfleet.exceptions import (
    FleetApiError,
    FleetAuthError,
    FleetError,
    FleetNotFoundError,
    FleetTimeoutError,
)
from agentfleet.models import InstanceInfo, SwarmTeamInfo, TaskInfo
from agentfleet.bot import Bot
from agentfleet.task import Task
from agentfleet.swarm import Swarm


# --------------------------------------------------------------- transport ---


class _FakeResponse:
    """Stands in for the object `urllib.request.urlopen` yields."""

    def __init__(self, payload, status=200):
        self.status = status
        if payload is None:
            self._raw = b""
        elif isinstance(payload, (bytes, bytearray)):
            self._raw = bytes(payload)
        else:
            self._raw = json.dumps(payload).encode("utf-8")

    def read(self):
        return self._raw

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class TransportMixin:
    """Patches the one place the SDK touches the network.

    Everything below asserts against the real `urllib.request.Request` the
    client builds, so a change to method, path, headers or body shows up here
    rather than in production.
    """

    def setUp(self):
        super().setUp()
        self.requests = []
        self.responses = []
        patcher = patch("agentfleet.client.urllib.request.urlopen")
        self.urlopen = patcher.start()
        self.addCleanup(patcher.stop)
        self.urlopen.side_effect = self._urlopen

    def _urlopen(self, req, timeout=None):
        self.requests.append(req)
        self.timeout_used = timeout
        if not self.responses:
            return _FakeResponse({})
        nxt = self.responses.pop(0)
        if isinstance(nxt, Exception):
            raise nxt
        return nxt

    def queue(self, *responses):
        self.responses.extend(responses)

    @property
    def last(self):
        return self.requests[-1]

    def assert_call(self, method, path, body=None):
        req = self.last
        self.assertEqual(req.get_method(), method)
        self.assertEqual(req.full_url, "http://mock-fleet:8080" + path)
        if body is None:
            self.assertIsNone(req.data)
        else:
            self.assertEqual(json.loads(req.data.decode("utf-8")), body)


def _http_error(code, body):
    return urllib.error.HTTPError(
        "http://mock-fleet:8080/x", code, "err", {}, io.BytesIO(body.encode("utf-8"))
    )


# ------------------------------------------------------------ construction ---


class TestClientConstruction(unittest.TestCase):
    def test_default_base_url_and_timeout(self):
        c = FleetClient()
        self.assertEqual(c.base_url, "http://localhost:8080")
        self.assertEqual(c.timeout, 30.0)

    def test_trailing_slashes_are_normalised(self):
        for raw in (
            "http://mock-fleet:8080/",
            "http://mock-fleet:8080//",
            "http://mock-fleet:8080///",
        ):
            with self.subTest(raw=raw):
                self.assertEqual(
                    FleetClient(raw).base_url, "http://mock-fleet:8080"
                )

    def test_base_url_with_path_prefix_keeps_the_prefix(self):
        self.assertEqual(
            FleetClient("https://fleet.example.com/orchestrator/").base_url,
            "https://fleet.example.com/orchestrator",
        )

    def test_token_falls_back_to_environment(self):
        with patch.dict(os.environ, {"AGENTFLEET_TOKEN": "env-token"}, clear=False):
            self.assertEqual(FleetClient("http://x").token, "env-token")
            # An explicit token wins over the environment.
            self.assertEqual(
                FleetClient("http://x", token="explicit").token, "explicit"
            )

    def test_from_env_reads_url_and_token(self):
        env = {"AGENTFLEET_URL": "http://remote:9999/", "AGENTFLEET_TOKEN": "t0k"}
        with patch.dict(os.environ, env, clear=False):
            c = FleetClient.from_env()
            self.assertEqual(c.base_url, "http://remote:9999")
            self.assertEqual(c.token, "t0k")


# -------------------------------------------------------------------- auth ---


class TestAuthHeader(TransportMixin, unittest.TestCase):
    def setUp(self):
        super().setUp()
        self.client = FleetClient("http://mock-fleet:8080/", token="s3cr3t-token")

    def test_token_is_sent_as_bearer_on_every_request(self):
        self.queue(
            _FakeResponse({"status": "ok"}),
            _FakeResponse([]),
            _FakeResponse({"id": "i1"}),
            _FakeResponse(None, status=204),
        )
        self.client.health()
        self.client.list_bots()
        self.client.deploy_bot("cyber_ops", name="b")
        self.client._delete("/api/instances/i1")

        self.assertEqual(len(self.requests), 4)
        for req in self.requests:
            self.assertEqual(
                req.get_header("Authorization"), "Bearer s3cr3t-token"
            )
            self.assertEqual(req.get_header("Content-type"), "application/json")
            self.assertEqual(req.get_header("Accept"), "application/json")

    def test_no_authorization_header_when_unauthenticated(self):
        with patch.dict(os.environ, {}, clear=True):
            anon = FleetClient("http://mock-fleet:8080")
        self.queue(_FakeResponse({"status": "ok"}))
        anon.health()
        self.assertIsNone(self.last.get_header("Authorization"))

    def test_login_stores_token_and_posts_credentials(self):
        self.queue(_FakeResponse({"token": "fresh-token"}))
        token = self.client.login("op@example.com", "hunter2")
        self.assert_call(
            "POST",
            "/api/auth/login",
            {"email": "op@example.com", "password": "hunter2"},
        )
        self.assertEqual(token, "fresh-token")
        self.assertEqual(self.client.token, "fresh-token")

    def test_client_timeout_is_passed_to_the_transport(self):
        c = FleetClient("http://mock-fleet:8080", token="t", timeout=7.5)
        self.queue(_FakeResponse({}))
        c.health()
        self.assertEqual(self.timeout_used, 7.5)


# ------------------------------------------------------ request shape: bots ---


class TestDeployAndRunShape(TransportMixin, unittest.TestCase):
    def setUp(self):
        super().setUp()
        self.client = FleetClient("http://mock-fleet:8080", token="tok")

    def test_deploy_bot_request_shape(self):
        self.queue(
            _FakeResponse(
                {
                    "id": "inst-1",
                    "name": "auditor",
                    "tier": "developer-heavy",
                    "state": "running",
                    "archetype_id": "cyber_ops",
                }
            )
        )
        bot = self.client.deploy_bot(
            "cyber_ops",
            name="auditor",
            tier="developer-heavy",
            override={"cpu": 4},
            shell_access=False,
            system_prompt="be careful",
        )
        self.assert_call(
            "POST",
            "/api/instances",
            {
                "name": "auditor",
                "archetype_id": "cyber_ops",
                "tier": "developer-heavy",
                "override": {"cpu": 4},
                "shell_access": False,
                "egress": "all",
                "system_prompt": "be careful",
            },
        )
        self.assertIsInstance(bot, Bot)
        self.assertEqual(bot.id, "inst-1")

    def test_deploy_bot_generates_a_name_when_none_given(self):
        self.queue(_FakeResponse({"id": "inst-2", "name": "x"}))
        self.client.deploy_bot("fullstack_dev")
        body = json.loads(self.last.data.decode("utf-8"))
        self.assertTrue(body["name"].startswith("fullstack_dev-"))

    def test_bot_run_request_shape(self):
        info = InstanceInfo(id="inst-9", name="b", tier="standard", state="running")
        bot = Bot(self.client, info)
        self.queue(
            _FakeResponse(
                {"id": "task-1", "instance_id": "inst-9", "goal": "g", "state": "pending"}
            )
        )
        task = bot.run("g", max_steps=12, auto_refine=False, wait=False)
        self.assert_call(
            "POST",
            "/api/tasks",
            {
                "instance_id": "inst-9",
                "goal": "g",
                "max_steps": 12,
                "auto_refine": False,
            },
        )
        self.assertIsInstance(task, Task)
        self.assertEqual(task.id, "task-1")

    def test_bot_lifecycle_paths(self):
        info = InstanceInfo(id="inst-9", name="b", tier="standard", state="stopped")
        bot = Bot(self.client, info)

        self.queue(_FakeResponse({}), _FakeResponse({"id": "inst-9", "state": "running"}))
        bot.start()
        self.assertEqual(self.requests[0].get_method(), "POST")
        self.assertTrue(self.requests[0].full_url.endswith("/api/instances/inst-9/start"))
        self.assertEqual(self.requests[1].get_method(), "GET")

        self.queue(_FakeResponse(None, status=204))
        bot.delete()
        self.assert_call("DELETE", "/api/instances/inst-9")

    def test_get_task_unwraps_envelope(self):
        self.queue(_FakeResponse({"task": {"id": "t-7", "state": "succeeded"}}))
        task = self.client.get_task("t-7")
        self.assert_call("GET", "/api/tasks/t-7")
        self.assertEqual(task.id, "t-7")
        self.assertTrue(task.is_done)

    def test_list_tasks_filters_by_instance(self):
        self.queue(_FakeResponse([]))
        self.client.list_tasks("inst-9")
        self.assert_call("GET", "/api/tasks?instance_id=inst-9")

    def test_task_wait_raises_typed_timeout(self):
        info = TaskInfo(id="t-1", instance_id="i", goal="g", state="running")
        task = Task(self.client, info)
        self.queue(_FakeResponse({"id": "t-1", "state": "running"}))
        with self.assertRaises(FleetTimeoutError):
            task.wait(timeout=0.0, poll_interval=0.0)


# ------------------------------------------------------------------- comms ---


class TestSwarmComms(TransportMixin, unittest.TestCase):
    """The swarm blackboard is the only bot-to-bot messaging the SDK exposes."""

    def setUp(self):
        super().setUp()
        self.client = FleetClient("http://mock-fleet:8080", token="tok")
        self.swarm = Swarm(
            self.client,
            SwarmTeamInfo(id="sw-1", name="Team", mission="m", status="running"),
        )

    def test_launch_swarm_request_shape(self):
        self.queue(_FakeResponse({"id": "sw-2", "name": "T", "mission": "m"}))
        members = [{"archetype_id": "qa_tester", "role": "QA"}]
        swarm = self.client.launch_swarm("T", "m", members=members)
        self.assert_call(
            "POST", "/api/swarms", {"name": "T", "mission": "m", "members": members}
        )
        self.assertIsInstance(swarm, Swarm)

    def test_launch_swarm_defaults_members_to_empty_list(self):
        self.queue(_FakeResponse({"id": "sw-3"}))
        self.client.launch_swarm("T", "m")
        self.assertEqual(json.loads(self.last.data.decode("utf-8"))["members"], [])

    def test_broadcast_to_all(self):
        self.queue(
            _FakeResponse({"id": "m-1", "swarm_id": "sw-1", "content": "go"}),
            _FakeResponse({"id": "sw-1", "name": "Team", "mission": "m"}),
        )
        msg = self.swarm.broadcast("go")
        self.assertEqual(self.requests[0].get_method(), "POST")
        self.assertEqual(
            self.requests[0].full_url,
            "http://mock-fleet:8080/api/swarms/sw-1/messages",
        )
        self.assertEqual(
            json.loads(self.requests[0].data.decode("utf-8")),
            {
                "from_bot": "Mission Operator",
                "to_bot": "all",
                "phase": "execution",
                "content": "go",
            },
        )
        self.assertEqual(msg.id, "m-1")
        # broadcast() refreshes the blackboard afterwards.
        self.assertEqual(self.requests[1].get_method(), "GET")

    def test_peer_directed_message(self):
        self.queue(
            _FakeResponse({"id": "m-2", "to_bot": "QA Bot"}),
            _FakeResponse({"id": "sw-1"}),
        )
        msg = self.swarm.broadcast(
            "please retest login",
            from_bot="Lead Architect",
            to_bot="QA Bot",
            phase="review",
        )
        self.assertEqual(
            json.loads(self.requests[0].data.decode("utf-8")),
            {
                "from_bot": "Lead Architect",
                "to_bot": "QA Bot",
                "phase": "review",
                "content": "please retest login",
            },
        )
        self.assertEqual(msg.to_bot, "QA Bot")

    def test_speak_hits_the_real_endpoint_and_returns_audio(self):
        # These assertions used to pin the bug: the old test asserted the path
        # /voice/speak (no /api prefix — a guaranteed 404 against the server)
        # and a fabricated {"status": "spoken"} fallback. The endpoint streams
        # WAV, so the contract is bytes in, bytes out.
        wav = b"RIFF....WAVEfmt "
        self.queue(_FakeResponse(wav))
        got = self.client.speak("all clear", voice="aura")
        self.assert_call("POST", "/api/voice/speak", {"text": "all clear", "voice": "aura"})
        self.assertEqual(got, wav)
        self.assertIsInstance(got, bytes)

    def test_speak_passes_speed_only_when_set(self):
        self.queue(_FakeResponse(b"RIFF"))
        self.client.speak("hi", voice="shadow", speed=1.5)
        self.assert_call(
            "POST", "/api/voice/speak", {"text": "hi", "voice": "shadow", "speed": 1.5}
        )

    def test_voices_reports_availability(self):
        self.queue(_FakeResponse({"available": False, "reason": "no sidecar", "voices": []}))
        got = self.client.voices()
        self.assert_call("GET", "/api/voice/voices", None)
        self.assertFalse(got["available"])


# ------------------------------------------------------------------ errors ---


class TestErrorMapping(TransportMixin, unittest.TestCase):
    def setUp(self):
        super().setUp()
        self.client = FleetClient("http://mock-fleet:8080", token="tok")

    def test_401_and_403_raise_auth_error(self):
        for code in (401, 403):
            with self.subTest(code=code):
                self.queue(_http_error(code, '{"error":"token expired"}'))
                with self.assertRaises(FleetAuthError) as ctx:
                    self.client.list_bots()
                self.assertEqual(ctx.exception.status_code, code)
                self.assertIn("token expired", str(ctx.exception))

    def test_404_raises_not_found(self):
        self.queue(_http_error(404, '{"error":"no such instance"}'))
        with self.assertRaises(FleetNotFoundError) as ctx:
            self.client.get_bot("nope")
        self.assertEqual(ctx.exception.status_code, 404)

    def test_500_raises_api_error_not_none(self):
        self.queue(_http_error(500, '{"error":"provisioning failed"}'))
        with self.assertRaises(FleetApiError) as ctx:
            self.client.deploy_bot("cyber_ops")
        self.assertEqual(ctx.exception.status_code, 500)
        self.assertEqual(ctx.exception.message, "provisioning failed")

    def test_non_json_error_body_is_preserved(self):
        self.queue(_http_error(502, "<html>bad gateway</html>"))
        with self.assertRaises(FleetApiError) as ctx:
            self.client.health()
        self.assertIn("bad gateway", str(ctx.exception))

    def test_connection_failure_raises_api_error(self):
        self.queue(urllib.error.URLError("connection refused"))
        with self.assertRaises(FleetApiError) as ctx:
            self.client.health()
        self.assertIn("Connection failed", str(ctx.exception))

    def test_error_hierarchy(self):
        self.assertTrue(issubclass(FleetApiError, FleetError))
        self.assertTrue(issubclass(FleetAuthError, FleetApiError))
        self.assertTrue(issubclass(FleetNotFoundError, FleetApiError))
        self.assertTrue(issubclass(FleetTimeoutError, FleetError))

    def test_204_returns_none_and_empty_body_returns_none(self):
        self.queue(_FakeResponse(None, status=204), _FakeResponse(b""))
        self.assertIsNone(self.client._delete("/api/instances/x"))
        self.assertIsNone(self.client._get("/api/instances/y"))


# ---------------------------------------------------------- secret leakage ---


SECRET = "sk-live-DO-NOT-LEAK-9f3a2b"


class TestSecretsNeverLeak(TransportMixin, unittest.TestCase):
    """A credential handed to the client must not resurface in any string.

    The client is routinely printed in tracebacks and logs; an exception is
    routinely forwarded to a model. Either one carrying a live token is a
    disclosure, so both are asserted here rather than assumed.
    """

    def setUp(self):
        super().setUp()
        self.client = FleetClient("http://mock-fleet:8080", token=SECRET)

    def test_token_absent_from_repr_and_str(self):
        self.assertNotIn(SECRET, repr(self.client))
        self.assertNotIn(SECRET, str(self.client))

    def test_token_absent_from_vars_dump_is_not_claimed(self):
        # Honest scope: __dict__ obviously holds the token; what must stay
        # clean is the *formatted* representation, checked above.
        self.assertIn("token", vars(self.client))

    def test_secret_absent_from_api_error(self):
        self.queue(_http_error(500, '{"error":"upstream rejected the request"}'))
        with self.assertRaises(FleetApiError) as ctx:
            self.client._post("/api/vault/secrets", {"key": "k", "value": SECRET})
        rendered = "%r %s" % (ctx.exception, ctx.exception)
        self.assertNotIn(SECRET, rendered)
        self.assertNotIn(SECRET, ctx.exception.message)

    def test_secret_absent_from_connection_error(self):
        self.queue(urllib.error.URLError("connection refused"))
        with self.assertRaises(FleetApiError) as ctx:
            self.client._post("/api/vault/secrets", {"value": SECRET})
        self.assertNotIn(SECRET, str(ctx.exception))

    def test_password_absent_from_login_failure(self):
        self.queue(_http_error(401, '{"error":"invalid credentials"}'))
        with self.assertRaises(FleetAuthError) as ctx:
            self.client.login("op@example.com", SECRET)
        self.assertNotIn(SECRET, str(ctx.exception))

    def test_secret_absent_from_bot_and_task_repr(self):
        self.queue(_FakeResponse({"id": "i1", "name": "n"}))
        bot = self.client.deploy_bot("cyber_ops", name="n", system_prompt=SECRET)
        self.assertNotIn(SECRET, repr(bot))
        task = Task(self.client, TaskInfo(id="t", instance_id="i", goal="g", state="x"))
        self.assertNotIn(SECRET, repr(task))


# ------------------------------------------------------- documented surface ---


class TestDocumentedSurface(unittest.TestCase):
    """Guards the API the README and pyproject promise."""

    def test_readme_quickstart_symbols_are_importable(self):
        self.assertIs(agentfleet.FleetClient, FleetClient)
        for name in ("deploy_bot", "launch_swarm", "speak"):
            self.assertTrue(callable(getattr(FleetClient, name)), name)
        self.assertTrue(callable(getattr(Bot, "run")))

    def test_fleetctl_entry_point_resolves(self):
        from agentfleet.cli import main

        self.assertTrue(callable(main))

    def test_distribution_name_is_open_agent_fleet(self):
        # The pip name and the import name deliberately differ: `agentfleet`
        # on PyPI belongs to an unrelated project, so the distribution is
        # `open-agent-fleet` while the module everyone imports (and the
        # `fleetctl` entry point) stay `agentfleet`.
        import pathlib
        import re

        root = pathlib.Path(__file__).resolve().parents[1]
        text = (root / "pyproject.toml").read_text(encoding="utf-8")
        self.assertRegex(text, r'name\s*=\s*"open-agent-fleet"')
        self.assertRegex(text, r'fleetctl\s*=\s*"agentfleet\.cli:main"')

    def test_all_exports_actually_exist(self):
        for name in agentfleet.__all__:
            self.assertTrue(hasattr(agentfleet, name), name)


class TestVaultAndCommsSurface(unittest.TestCase):
    """The orchestrator serves /api/vault/secrets and /api/vault/comms.

    The Python SDK exposes neither. These tests assert the gap explicitly so
    it cannot be mistaken for coverage, and the skipped bodies below activate
    on their own the moment the methods land.
    """

    VAULT_METHODS = ("vault_publish", "publish_secret", "vault_put", "put_secret")
    COMMS_METHODS = ("send_peer_message", "peer_message", "list_peer_messages")

    def test_vault_methods_are_not_implemented(self):
        present = [m for m in self.VAULT_METHODS if hasattr(FleetClient, m)]
        self.assertEqual(
            present,
            [],
            "FleetClient gained vault methods %s - replace this test with real "
            "request-shape assertions against POST/DELETE /api/vault/secrets"
            % present,
        )

    def test_comms_methods_are_not_implemented(self):
        present = [m for m in self.COMMS_METHODS if hasattr(FleetClient, m)]
        self.assertEqual(
            present,
            [],
            "FleetClient gained comms methods %s - replace this test with real "
            "request-shape assertions against GET/POST /api/vault/comms" % present,
        )

    @unittest.skipUnless(
        any(hasattr(FleetClient, m) for m in VAULT_METHODS),
        "FleetClient has no vault method; /api/vault/secrets is unreachable "
        "from the Python SDK",
    )
    def test_vault_publish_request_shape(self):  # pragma: no cover
        self.fail("implement once a vault method exists")

    @unittest.skipUnless(
        any(hasattr(FleetClient, m) for m in COMMS_METHODS),
        "FleetClient has no comms method; /api/vault/comms is unreachable "
        "from the Python SDK",
    )
    def test_peer_message_request_shape(self):  # pragma: no cover
        self.fail("implement once a comms method exists")


class TestOpenAgentFleetSDK(unittest.TestCase):
    def setUp(self):
        self.client = FleetClient("http://mock-fleet:8080", token="mock-token")

    def test_instance_model_parsing(self):
        data = {
            "id": "inst-cyber-01",
            "name": "RedTeam-01",
            "tier": "standard",
            "state": "running",
            "archetype_id": "cyber_ops",
            "shell_access": True,
            "preinstalled_tools": ["nmap", "wireshark"],
        }
        info = InstanceInfo.from_dict(data)
        self.assertEqual(info.id, "inst-cyber-01")
        self.assertEqual(info.archetype_id, "cyber_ops")
        self.assertTrue(info.shell_access)
        self.assertEqual(len(info.preinstalled_tools), 2)

    def test_task_model_parsing(self):
        data = {
            "id": "task-101",
            "instance_id": "inst-cyber-01",
            "goal": "Scan network",
            "state": "succeeded",
            "step": 4,
            "max_steps": 60,
            "result": "Port 80 open",
        }
        info = TaskInfo.from_dict(data)
        self.assertEqual(info.id, "task-101")
        self.assertEqual(info.state, "succeeded")
        self.assertEqual(info.result, "Port 80 open")

    def test_swarm_model_parsing(self):
        data = {
            "id": "swarm-99",
            "name": "Audit Swarm",
            "mission": "Pen test login flow",
            "status": "running",
            "members": [
                {
                    "instance_id": "inst-1",
                    "instance_name": "FullStack Bot",
                    "role": "Lead Architect",
                    "archetype_id": "fullstack_dev",
                    "status": "working",
                }
            ],
            "messages": [
                {
                    "id": "msg-1",
                    "swarm_id": "swarm-99",
                    "from_bot": "FullStack Bot",
                    "to_bot": "all",
                    "phase": "planning",
                    "content": "Architecture plan approved.",
                    "created_at": "2026-08-28T12:00:00Z",
                }
            ],
        }
        info = SwarmTeamInfo.from_dict(data)
        self.assertEqual(info.id, "swarm-99")
        self.assertEqual(len(info.members), 1)
        self.assertEqual(len(info.messages), 1)
        self.assertEqual(info.messages[0].phase, "planning")

    @patch.object(FleetClient, "_post")
    def test_deploy_bot(self, mock_post):
        mock_post.return_value = {
            "id": "inst-123",
            "name": "nightly-auditor",
            "tier": "standard",
            "state": "running",
            "archetype_id": "cyber_ops",
        }

        bot = self.client.deploy_bot("cyber_ops", name="nightly-auditor")
        self.assertEqual(bot.id, "inst-123")
        self.assertEqual(bot.name, "nightly-auditor")
        self.assertEqual(bot.archetype_id, "cyber_ops")
        mock_post.assert_called_once()

    @patch.object(FleetClient, "_post")
    def test_run_task(self, mock_post):
        mock_post.return_value = {
            "id": "task-555",
            "instance_id": "inst-123",
            "goal": "Test goal",
            "state": "pending",
        }
        bot_info = InstanceInfo(id="inst-123", name="test-bot", tier="standard", state="running")
        bot = Bot(self.client, bot_info)
        task = bot.run("Test goal", wait=False)
        self.assertEqual(task.id, "task-555")
        self.assertEqual(task.goal, "Test goal")

    @patch.object(FleetClient, "_post")
    def test_launch_swarm(self, mock_post):
        mock_post.return_value = {
            "id": "swarm-001",
            "name": "App Security Team",
            "mission": "Run penetration test and accessibility verification",
            "status": "running",
            "members": [],
            "messages": [],
        }
        swarm = self.client.launch_swarm("App Security Team", "Run penetration test and accessibility verification")
        self.assertEqual(swarm.id, "swarm-001")
        self.assertEqual(swarm.name, "App Security Team")

    def test_archetype_manifest_serialization(self):
        from agentfleet.hub import ArchetypeManifest

        manifest = ArchetypeManifest(
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
        d = manifest.to_dict()
        self.assertEqual(d["id"], "solana_auditor")
        self.assertEqual(d["name"], "Solana Smart Contract Auditor")
        loaded = ArchetypeManifest.from_dict(d)
        self.assertEqual(loaded.id, manifest.id)
        self.assertEqual(loaded.vcpu, 8.0)

    @patch.object(FleetClient, "_get")
    @patch.object(FleetClient, "_post")
    def test_list_and_reorder_providers(self, mock_post, mock_get):
        fake_providers = [
            {"id": "prov-claude", "name": "Claude 3.7", "priority": 10},
            {"id": "prov-antigravity", "name": "Google Antigravity", "priority": 20},
            {"id": "prov-ollama", "name": "Local Ollama", "priority": 30},
        ]
        mock_get.return_value = fake_providers
        res = self.client.list_providers()
        mock_get.assert_called_once_with("/api/providers")
        self.assertEqual(len(res), 3)

        mock_post.return_value = fake_providers
        reordered = self.client.reorder_providers(["prov-antigravity", "prov-claude", "prov-ollama"])
        mock_post.assert_called_once_with("/api/providers/reorder", {"ids": ["prov-antigravity", "prov-claude", "prov-ollama"]})
        self.assertEqual(len(reordered), 3)


if __name__ == "__main__":
    unittest.main()

