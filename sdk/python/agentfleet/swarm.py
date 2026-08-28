"""Multi-agent collaborative swarm abstraction."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, Optional

from agentfleet.models import SwarmTeamInfo, SwarmMessageInfo

if TYPE_CHECKING:
    from agentfleet.client import FleetClient


class Swarm:
    """Represents a collaborative multi-bot team swarm operating on a shared blackboard."""

    def __init__(self, client: FleetClient, info: SwarmTeamInfo) -> None:
        self._client = client
        self.info = info

    @property
    def id(self) -> str:
        return self.info.id

    @property
    def name(self) -> str:
        return self.info.name

    @property
    def mission(self) -> str:
        return self.info.mission

    @property
    def status(self) -> str:
        return self.info.status

    @property
    def members(self) -> list[Any]:
        return self.info.members

    @property
    def messages(self) -> list[SwarmMessageInfo]:
        return self.info.messages

    def refresh(self) -> Swarm:
        """Fetches the latest swarm and blackboard state."""
        data = self._client._get(f"/api/swarms/{self.id}")
        self.info = SwarmTeamInfo.from_dict(data)
        return self

    def broadcast(
        self,
        content: str,
        from_bot: str = "Mission Operator",
        to_bot: str = "all",
        phase: str = "execution",
    ) -> SwarmMessageInfo:
        """Broadcasts a directive or message to the swarm blackboard."""
        payload = {
            "from_bot": from_bot,
            "to_bot": to_bot,
            "phase": phase,
            "content": content,
        }
        res = self._client._post(f"/api/swarms/{self.id}/messages", payload)
        msg = SwarmMessageInfo.from_dict(res)
        self.refresh()
        return msg
