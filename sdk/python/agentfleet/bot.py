"""Bot instance abstraction."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, Optional

from agentfleet.models import InstanceInfo, TaskInfo
from agentfleet.task import Task

if TYPE_CHECKING:
    from agentfleet.client import FleetClient


class Bot:
    """Represents a provisioned sandbox bot instance."""

    def __init__(self, client: FleetClient, info: InstanceInfo) -> None:
        self._client = client
        self.info = info

    @property
    def id(self) -> str:
        return self.info.id

    @property
    def name(self) -> str:
        return self.info.name

    @property
    def archetype_id(self) -> Optional[str]:
        return self.info.archetype_id

    @property
    def state(self) -> str:
        return self.info.state

    @property
    def tier(self) -> str:
        return self.info.tier

    def refresh(self) -> Bot:
        """Fetches the latest state from the orchestrator."""
        data = self._client._get(f"/api/instances/{self.id}")
        self.info = InstanceInfo.from_dict(data)
        return self

    def start(self) -> Bot:
        """Starts the sandbox container."""
        self._client._post(f"/api/instances/{self.id}/start")
        return self.refresh()

    def stop(self) -> Bot:
        """Stops the sandbox container."""
        self._client._post(f"/api/instances/{self.id}/stop")
        return self.refresh()

    def delete(self) -> None:
        """Destroys and removes the sandbox container."""
        self._client._delete(f"/api/instances/{self.id}")

    def run(
        self,
        goal: str,
        max_steps: int = 60,
        auto_refine: bool = True,
        wait: bool = False,
        timeout: float = 600.0,
    ) -> Task:
        """Dispatches an autonomous goal to this bot."""
        payload = {
            "instance_id": self.id,
            "goal": goal,
            "max_steps": max_steps,
            "auto_refine": auto_refine,
        }
        res = self._client._post("/api/tasks", payload)
        task = Task(self._client, TaskInfo.from_dict(res))
        if wait:
            task.wait(timeout=timeout)
        return task

    def observe(self) -> dict[str, Any]:
        """Captures a live visual frame and active window telemetry."""
        return self._client._get(f"/api/instances/{self.id}/observe")

    def stats(self) -> dict[str, Any]:
        """Gets CPU, RAM, and network I/O statistics."""
        return self._client._get(f"/api/instances/{self.id}/stats")
