"""Data models for OpenAgentFleet SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class InstanceInfo:
    id: str
    name: str
    tier: str
    state: str
    archetype_id: Optional[str] = None
    system_prompt: Optional[str] = None
    preinstalled_tools: list[str] = field(default_factory=list)
    shell_access: bool = False
    vnc_url: str = ""
    last_error: str = ""
    created_at: str = ""
    # Where the agent sits and what it is: a desktop this fleet provisions, or
    # an external agent (claude_code, codex, hermes, openclaw, webhook).
    kind: str = "desktop"
    title: str = ""
    reports_to: str = ""
    capabilities: str = ""
    trust: str = "standard"
    hold: str = ""
    budget_month_usd: float = 0.0
    connection: dict[str, Any] = field(default_factory=dict)

    @property
    def external(self) -> bool:
        return self.kind not in ("", "desktop")

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> InstanceInfo:
        return cls(
            id=data.get("id", ""),
            name=data.get("name", ""),
            tier=data.get("tier", "standard"),
            state=data.get("state", "stopped"),
            archetype_id=data.get("archetype_id"),
            system_prompt=data.get("system_prompt"),
            preinstalled_tools=data.get("preinstalled_tools", []) or [],
            shell_access=data.get("shell_access", False),
            vnc_url=data.get("vnc_url", ""),
            last_error=data.get("last_error", ""),
            created_at=data.get("created_at", ""),
            kind=data.get("kind") or "desktop",
            title=data.get("title", "") or "",
            reports_to=data.get("reports_to", "") or "",
            capabilities=data.get("capabilities", "") or "",
            trust=data.get("trust") or "standard",
            hold=data.get("hold", "") or "",
            budget_month_usd=float(data.get("budget_month_usd") or 0),
            connection=data.get("connection") or {},
        )


@dataclass
class TaskInfo:
    id: str
    instance_id: str
    goal: str
    state: str
    step: int = 0
    max_steps: int = 60
    error: str = ""
    result: str = ""
    created_at: str = ""

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> TaskInfo:
        return cls(
            id=data.get("id", ""),
            instance_id=data.get("instance_id", ""),
            goal=data.get("goal", ""),
            state=data.get("state", "pending"),
            step=data.get("step", 0),
            max_steps=data.get("max_steps", 60),
            error=data.get("error", ""),
            result=data.get("result", ""),
            created_at=data.get("created_at", ""),
        )


@dataclass
class SwarmMemberInfo:
    instance_id: str
    instance_name: str
    role: str
    archetype_id: str
    status: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> SwarmMemberInfo:
        return cls(
            instance_id=data.get("instance_id", ""),
            instance_name=data.get("instance_name", ""),
            role=data.get("role", ""),
            archetype_id=data.get("archetype_id", ""),
            status=data.get("status", "working"),
        )


@dataclass
class SwarmMessageInfo:
    id: str
    swarm_id: str
    from_bot: str
    to_bot: str
    phase: str
    content: str
    created_at: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> SwarmMessageInfo:
        return cls(
            id=data.get("id", ""),
            swarm_id=data.get("swarm_id", ""),
            from_bot=data.get("from_bot", ""),
            to_bot=data.get("to_bot", ""),
            phase=data.get("phase", ""),
            content=data.get("content", ""),
            created_at=data.get("created_at", ""),
        )


@dataclass
class SwarmTeamInfo:
    id: str
    name: str
    mission: str
    status: str
    members: list[SwarmMemberInfo] = field(default_factory=list)
    messages: list[SwarmMessageInfo] = field(default_factory=list)
    created_at: str = ""

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> SwarmTeamInfo:
        return cls(
            id=data.get("id", ""),
            name=data.get("name", ""),
            mission=data.get("mission", ""),
            status=data.get("status", "running"),
            members=[SwarmMemberInfo.from_dict(m) for m in data.get("members", [])],
            messages=[SwarmMessageInfo.from_dict(m) for m in data.get("messages", [])],
            created_at=data.get("created_at", ""),
        )
