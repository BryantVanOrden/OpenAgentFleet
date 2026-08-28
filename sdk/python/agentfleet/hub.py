"""Portable .agentfleet.yaml Archetype Packager & Hub.

Exports and imports complete specialized bot archetypes including persona prompts,
hardware requirements, tool packages, environment presets, and recorded skills.
"""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Optional


@dataclass
class ArchetypeManifest:
    version: str
    id: str
    name: str
    tagline: str
    category: str
    recommended_tier: str
    vcpu: float
    memory_mb: int
    disk_gb: int
    gpu: bool
    preinstalled_tools: list[str]
    system_prompt: str
    default_environment: dict[str, str]
    mcp_servers: list[dict[str, Any]]
    recorded_skills: list[dict[str, Any]]

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ArchetypeManifest:
        return cls(
            version=data.get("version", "1.0.0"),
            id=data["id"],
            name=data["name"],
            tagline=data.get("tagline", ""),
            category=data.get("category", "General"),
            recommended_tier=data.get("recommended_tier", "standard"),
            vcpu=float(data.get("vcpu", 4.0)),
            memory_mb=int(data.get("memory_mb", 8192)),
            disk_gb=int(data.get("disk_gb", 30)),
            gpu=bool(data.get("gpu", False)),
            preinstalled_tools=data.get("preinstalled_tools", []),
            system_prompt=data.get("system_prompt", ""),
            default_environment=data.get("default_environment", {}),
            mcp_servers=data.get("mcp_servers", []),
            recorded_skills=data.get("recorded_skills", []),
        )

    def save(self, filepath: str | Path) -> None:
        """Save archetype manifest to JSON/YAML format."""
        path = Path(filepath)
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(self.to_dict(), f, indent=2)

    @classmethod
    def load(cls, filepath: str | Path) -> ArchetypeManifest:
        with open(filepath, "r", encoding="utf-8") as f:
            data = json.load(f)
        return cls.from_dict(data)
