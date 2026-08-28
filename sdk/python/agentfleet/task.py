"""Task execution abstraction."""

from __future__ import annotations

import time
from typing import TYPE_CHECKING, Any, Optional

from agentfleet.exceptions import FleetTimeoutError
from agentfleet.models import TaskInfo

if TYPE_CHECKING:
    from agentfleet.client import FleetClient


class Task:
    """Represents an autonomous goal execution on an instance."""

    def __init__(self, client: FleetClient, info: TaskInfo) -> None:
        self._client = client
        self.info = info

    @property
    def id(self) -> str:
        return self.info.id

    @property
    def instance_id(self) -> str:
        return self.info.instance_id

    @property
    def goal(self) -> str:
        return self.info.goal

    @property
    def state(self) -> str:
        return self.info.state

    @property
    def result(self) -> str:
        return self.info.result

    @property
    def error(self) -> str:
        return self.info.error

    @property
    def is_done(self) -> bool:
        return self.state in ("succeeded", "failed", "cancelled")

    def refresh(self) -> Task:
        """Fetches the latest execution status from the orchestrator."""
        data = self._client._get(f"/api/tasks/{self.id}")
        tdata = data.get("task", data)
        self.info = TaskInfo.from_dict(tdata)
        return self

    def cancel(self) -> None:
        """Cancels this running task."""
        self._client._post(f"/api/tasks/{self.id}/cancel")
        self.refresh()

    def get_steps(self) -> list[dict[str, Any]]:
        """Returns the full execution step trajectory."""
        return self._client._get(f"/api/tasks/{self.id}/steps")

    def wait(self, timeout: float = 600.0, poll_interval: float = 1.5) -> Task:
        """Blocks until the task completes or the timeout expires."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.refresh()
            if self.is_done:
                return self
            time.sleep(poll_interval)
        raise FleetTimeoutError(f"Task {self.id} timed out after {timeout}s (state: {self.state})")
