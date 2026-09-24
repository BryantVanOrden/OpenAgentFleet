"""OpenAgentFleet orchestrator client."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Optional

from agentfleet.bot import Bot
from agentfleet.exceptions import FleetApiError, FleetAuthError, FleetNotFoundError
from agentfleet.models import InstanceInfo, SwarmTeamInfo, TaskInfo
from agentfleet.swarm import Swarm
from agentfleet.task import Task


class FleetClient:
    """Client for communicating with the OpenAgentFleet orchestrator API."""

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        token: Optional[str] = None,
        timeout: float = 30.0,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.token = token or os.environ.get("AGENTFLEET_TOKEN")
        self.timeout = timeout

    @classmethod
    def from_env(cls) -> FleetClient:
        """Initializes client from AGENTFLEET_URL and AGENTFLEET_TOKEN environment variables."""
        url = os.environ.get("AGENTFLEET_URL", "http://localhost:8080")
        token = os.environ.get("AGENTFLEET_TOKEN")
        return cls(base_url=url, token=token)

    # ------------------------------------------------------------------ HTTP ---

    def _request(
        self,
        method: str,
        path: str,
        data: Optional[dict[str, Any]] = None,
        raw: bool = False,
    ) -> Any:
        """One HTTP round trip. With raw=True the body comes back as bytes.

        raw exists for the one endpoint that does not speak JSON:
        /api/voice/speak streams WAV audio, and decoding that as UTF-8 to
        json.loads it is an exception, not a response.
        """
        url = f"{self.base_url}{path}"
        headers = {"Content-Type": "application/json", "Accept": "application/json"}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        body = json.dumps(data).encode("utf-8") if data is not None else None
        req = urllib.request.Request(url, data=body, headers=headers, method=method)

        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                status = resp.status
                if status == 204:
                    return None
                if raw:
                    return resp.read()
                raw_body = resp.read().decode("utf-8")
                return json.loads(raw_body) if raw_body else None
        except urllib.error.HTTPError as exc:
            err_body = exc.read().decode("utf-8")
            msg = err_body
            try:
                msg = json.loads(err_body).get("error", err_body)
            except Exception:
                pass

            if exc.code == 401 or exc.code == 403:
                raise FleetAuthError(msg, status_code=exc.code) from exc
            if exc.code == 404:
                raise FleetNotFoundError(msg, status_code=exc.code) from exc
            raise FleetApiError(msg, status_code=exc.code) from exc
        except urllib.error.URLError as exc:
            raise FleetApiError(f"Connection failed: {exc.reason}") from exc

    def _get(self, path: str) -> Any:
        return self._request("GET", path)

    def _post(self, path: str, data: Optional[dict[str, Any]] = None) -> Any:
        return self._request("POST", path, data=data)

    def _delete(self, path: str) -> Any:
        return self._request("DELETE", path)

    def _put(self, path: str, data: Optional[dict[str, Any]] = None) -> Any:
        return self._request("PUT", path, data=data)

    def _patch(self, path: str, data: Optional[dict[str, Any]] = None) -> Any:
        return self._request("PATCH", path, data=data)

    # ------------------------------------------------------------- Tickets ---
    # Work as durable tickets: one assignee, a parent, and what it waits on.
    # A ticket is addressed by its id or its reference ("T-12").

    def list_tickets(self, status: Optional[list[str]] = None, assignee: Optional[str] = None,
                     roots: bool = False, limit: int = 200) -> list[dict[str, Any]]:
        """Tickets, newest first. status filters by one or more statuses."""
        q = [f"limit={int(limit)}"]
        if status:
            q.append("status=" + ",".join(status))
        if assignee:
            q.append("assignee=" + urllib.parse.quote(assignee))
        if roots:
            q.append("roots=1")
        return self._get("/api/tickets?" + "&".join(q)) or []

    def create_ticket(self, title: str, description: str = "", assignee_id: Optional[str] = None,
                      parent: Optional[str] = None, blocked_by: Optional[list[str]] = None,
                      reviewer_id: Optional[str] = None, verifier_id: Optional[str] = None,
                      budget_usd: float = 0, kind: str = "work") -> dict[str, Any]:
        """Files a ticket. It starts as soon as its blockers are done and its assignee is free."""
        body: dict[str, Any] = {"title": title, "description": description, "kind": kind}
        for k, v in (("assignee_id", assignee_id), ("parent_id", parent), ("reviewer_id", reviewer_id),
                     ("verifier_id", verifier_id)):
            if v:
                body[k] = v
        if blocked_by:
            body["blocked_by"] = list(blocked_by)
        if budget_usd:
            body["budget_usd"] = budget_usd
        return self._post("/api/tickets", body)

    def ticket(self, ref: str) -> dict[str, Any]:
        """A ticket with its ancestry, children, blockers, comments and runs."""
        return self._get(f"/api/tickets/{urllib.parse.quote(ref)}")

    def update_ticket(self, ref: str, **fields: Any) -> dict[str, Any]:
        """Changes any of title, description, status, priority, assignee_id,
        reviewer_id, verifier_id, parent_id, budget_usd, blocked_by."""
        return self._patch(f"/api/tickets/{urllib.parse.quote(ref)}", fields)

    def comment_ticket(self, ref: str, body: str) -> dict[str, Any]:
        return self._post(f"/api/tickets/{urllib.parse.quote(ref)}/comments", {"body": body})

    def reopen_ticket(self, ref: str, reason: str) -> dict[str, Any]:
        """Sends finished work back to its assignee with what is missing."""
        return self._post(f"/api/tickets/{urllib.parse.quote(ref)}/reopen", {"reason": reason})

    def delete_ticket(self, ref: str) -> None:
        self._delete(f"/api/tickets/{urllib.parse.quote(ref)}")

    # ----------------------------------------------------------- Org chart ---

    def org(self) -> dict[str, Any]:
        """The org chart: every agent, who it reports to, and what it is doing."""
        return self._get("/api/org")

    def set_profile(self, instance_id: str, **fields: Any) -> dict[str, Any]:
        """Changes an agent's title, capabilities, reports_to, trust, budget
        (budget_month_usd, budget_warn_pct; admin only) or connection."""
        return self._put(f"/api/instances/{instance_id}/profile", fields)

    def add_external_agent(self, name: str, kind: str, connection: dict[str, Any],
                           token: Optional[str] = None, **fields: Any) -> dict[str, Any]:
        """Adds a Claude Code, Codex, Hermes, OpenClaw or webhook agent.

        For the CLI kinds, connection is {"device_id", "cwd", "model", "autonomy"};
        for OpenClaw {"url", "agent_id"}; for a webhook {"url"}.
        """
        body: dict[str, Any] = {"name": name, "kind": kind, "connection": connection, **fields}
        if token:
            body["token"] = token
        return self._post("/api/instances", body)

    # ------------------------------------------------------------------ Auth ---

    def login(self, email: str, password: str) -> str:
        """Authenticates with email/password and saves the session token."""
        res = self._post("/api/auth/login", {"email": email, "password": password})
        self.token = res.get("token")
        return self.token or ""

    def health(self) -> dict[str, Any]:
        """Probes orchestrator health status."""
        return self._get("/healthz")

    # ------------------------------------------------------------- Instances ---

    def list_bots(self) -> list[Bot]:
        """Lists all active and stopped bot instances."""
        data = self._get("/api/instances") or []
        return [Bot(self, InstanceInfo.from_dict(d)) for d in data]

    def get_bot(self, instance_id: str) -> Bot:
        """Fetches a specific bot by instance ID."""
        data = self._get(f"/api/instances/{instance_id}")
        return Bot(self, InstanceInfo.from_dict(data))

    def deploy_bot(
        self,
        archetype_id: str = "fullstack_dev",
        name: Optional[str] = None,
        tier: str = "standard",
        override: Optional[dict[str, Any]] = None,
        shell_access: bool = True,
        system_prompt: Optional[str] = None,
    ) -> Bot:
        """Provisions a new hard-sandboxed bot instance tailored to an archetype."""
        bot_name = name or f"{archetype_id}-{os.urandom(2).hex()}"
        payload = {
            "name": bot_name,
            "archetype_id": archetype_id,
            "tier": tier,
            "override": override,
            "shell_access": shell_access,
            "egress": "all",
            "system_prompt": system_prompt,
        }
        res = self._post("/api/instances", payload)
        return Bot(self, InstanceInfo.from_dict(res))

    # ----------------------------------------------------------------- Tasks ---

    def list_tasks(self, instance_id: Optional[str] = None) -> list[Task]:
        """Lists all tasks across the fleet or for a specific instance."""
        path = f"/api/tasks?instance_id={instance_id}" if instance_id else "/api/tasks"
        data = self._get(path) or []
        return [Task(self, TaskInfo.from_dict(d)) for d in data]

    def get_task(self, task_id: str) -> Task:
        """Fetches task status and result."""
        data = self._get(f"/api/tasks/{task_id}")
        tdata = data.get("task", data)
        return Task(self, TaskInfo.from_dict(tdata))

    # ---------------------------------------------------------------- Swarms ---

    def list_swarms(self) -> list[Swarm]:
        """Lists all collaborative multi-agent swarms."""
        data = self._get("/api/swarms") or []
        return [Swarm(self, SwarmTeamInfo.from_dict(d)) for d in data]

    def get_swarm(self, swarm_id: str) -> Swarm:
        """Fetches a swarm by ID."""
        data = self._get(f"/api/swarms/{swarm_id}")
        return Swarm(self, SwarmTeamInfo.from_dict(data))

    def launch_swarm(
        self,
        name: str,
        mission: str,
        members: Optional[list[dict[str, Any]]] = None,
    ) -> Swarm:
        """Launches a collaborative multi-agent team swarm on a shared blackboard."""
        payload = {"name": name, "mission": mission, "members": members or []}
        res = self._post("/api/swarms", payload)
        return Swarm(self, SwarmTeamInfo.from_dict(res))

    # ----------------------------------------------------------------- Voice ---

    def speak(self, text: str, voice: str = "shadow", speed: float = 0) -> bytes:
        """Synthesize speech and return the WAV bytes.

        This never worked before: it posted to /voice/speak — no /api prefix, so
        a guaranteed 404 — and when the path was corrected the endpoint returns
        audio, which the JSON plumbing would have thrown on. The `or {...}`
        fallback then fabricated a success dict, which is the exact behaviour
        this codebase keeps having to unlearn. Callers get the audio or an
        exception; a fleet with no TTS sidecar raises FleetApiError with the
        server's explanation.
        """
        payload: dict[str, Any] = {"text": text, "voice": voice}
        if speed:
            payload["speed"] = speed
        return self._request("POST", "/api/voice/speak", data=payload, raw=True)

    def voices(self) -> dict[str, Any]:
        """The voices the fleet can produce, or {"available": false, ...}."""
        return self._get("/api/voice/voices")

    # ----------------------------------------------------------- MCP & Pipelines ---

    def list_mcp_servers(self) -> list[dict[str, Any]]:
        """Lists connected Model Context Protocol (MCP) servers."""
        return self._get("/api/mcp/servers") or []

    def register_mcp_server(
        self,
        name: str,
        transport: str = "stdio",
        command: Optional[str] = None,
        args: Optional[list[str]] = None,
        url: Optional[str] = None,
        env: Optional[dict[str, str]] = None,
    ) -> dict[str, Any]:
        """Register an MCP tool server. The orchestrator connects, completes the
        handshake and discovers tools before storing anything, so a server that
        cannot be reached is an exception here rather than a broken row.

        stdio: `command` is one executable and `args` its argv — the server is
        exec'd directly, not through a shell, so a full command line stuffed
        into `command` execs a binary with a space in its name and fails. (The
        old signature invited exactly that.) The process runs inside the
        orchestrator's container, so the executable has to exist there.

        http: pass `url`; `env` becomes extra request headers, which is how a
        bearer token is supplied.
        """
        payload: dict[str, Any] = {"name": name, "transport": transport}
        if command:
            payload["command"] = command
        if args:
            payload["args"] = args
        if url:
            payload["url"] = url
        if env:
            payload["env"] = env
        return self._post("/api/mcp/servers", payload) or {}

    def call_mcp_tool(
        self, tool_name: str, params: Optional[dict[str, Any]] = None, server_id: str = ""
    ) -> dict[str, Any]:
        """Invoke one MCP tool. server_id is optional — the orchestrator
        resolves the tool name to whichever server provides it."""
        return self._post(
            "/api/mcp/call",
            {"server_id": server_id, "tool_name": tool_name, "params": params or {}},
        ) or {}

    def list_pipelines(self) -> list[dict[str, Any]]:
        """Lists multi-bot workflow DAG pipelines."""
        return self._get("/api/pipelines") or []

    def run_pipeline(self, pipeline_id: str) -> dict[str, Any]:
        """Triggers execution of a workflow pipeline."""
        return self._post(f"/api/pipelines/{pipeline_id}/run") or {}

    def get_financial_summary(self) -> dict[str, Any]:
        """Fetches fleet-wide token and cost financial telemetry."""
        return self._get("/api/telemetry/financials") or {}

    # ----------------------------------------------------------- AI Providers & Fallback ---

    def list_providers(self) -> list[dict[str, Any]]:
        """Lists configured AI engine providers in tiered fallback priority order."""
        return self._get("/api/providers") or []

    def reorder_providers(self, provider_ids: list[str]) -> list[dict[str, Any]]:
        """Reorders the fallback chain priority (Tier 1 -> Tier 2 -> Tier 3)."""
        return self._post("/api/providers/reorder", {"ids": provider_ids}) or []

