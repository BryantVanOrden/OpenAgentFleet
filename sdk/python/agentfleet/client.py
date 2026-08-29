"""AgentFleet orchestrator client."""

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
    """Client for communicating with the AgentFleet orchestrator API."""

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
    ) -> Any:
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
                raw = resp.read().decode("utf-8")
                return json.loads(raw) if raw else None
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

    def speak(self, text: str, voice: str = "shadow") -> dict[str, Any]:
        """Synthesizes speech using Pocket TTS across 6 curated voice models."""
        payload = {"text": text, "voice": voice}
        return self._post("/voice/speak", payload) or {"status": "spoken", "voice": voice}

    # ----------------------------------------------------------- MCP & Pipelines ---

    def list_mcp_servers(self) -> list[dict[str, Any]]:
        """Lists connected Model Context Protocol (MCP) servers."""
        return self._get("/api/mcp/servers") or []

    def register_mcp_server(self, name: str, command: str, transport: str = "stdio") -> dict[str, Any]:
        """Registers a new Model Context Protocol tool server."""
        payload = {"name": name, "command": command, "transport": transport}
        return self._post("/api/mcp/servers", payload) or {}

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

