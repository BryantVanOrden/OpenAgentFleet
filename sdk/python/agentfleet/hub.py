"""Portable .agentfleet.yaml archetype packager.

Exports and imports complete specialized bot archetypes: persona prompts,
hardware requirements, tool packages, environment presets, registered MCP
servers and recorded skills.

What this used to be, stated plainly because the README listed it: `save()`
wrote JSON whatever the extension said, so the format this module is named after
was never produced; `tools` and `recorded_skills` were always empty because the
exporter never asked the fleet for them; and `hub import` read a file back,
printed three lines about it, and created nothing at all. There was no endpoint
behind any of it.

YAML is written without a third-party dependency. The SDK has no requirements
beyond the standard library and that is worth keeping for a package people pip
install into an agent's sandbox -- and a manifest is a flat mapping of scalars,
lists and short dicts, which is a small enough subset to emit correctly. Loading
prefers PyYAML when it is installed and falls back to the same subset.
"""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any


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
    # Optional, and defaulted so a manifest written by an older version still
    # loads. These come from the server's exporter, which knows things the SDK
    # cannot: the fleet's own skills and MCP registrations.
    icon: str = ""
    preinstalled_repos: list[str] = field(default_factory=list)
    default_voice: str = ""
    default_shell_access: bool = False

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ArchetypeManifest":
        if not data.get("id") or not data.get("name"):
            raise ValueError("a manifest needs an id and a name")
        return cls(
            version=str(data.get("version", "1.0.0")),
            id=str(data["id"]),
            name=str(data["name"]),
            tagline=str(data.get("tagline", "")),
            category=str(data.get("category", "General")),
            recommended_tier=str(data.get("recommended_tier", "standard")),
            vcpu=float(data.get("vcpu", 4.0)),
            memory_mb=int(data.get("memory_mb", 8192)),
            disk_gb=int(data.get("disk_gb", 30)),
            gpu=bool(data.get("gpu", False)),
            preinstalled_tools=list(data.get("preinstalled_tools") or []),
            system_prompt=str(data.get("system_prompt", "")),
            default_environment=dict(data.get("default_environment") or {}),
            mcp_servers=list(data.get("mcp_servers") or []),
            recorded_skills=list(data.get("recorded_skills") or []),
            icon=str(data.get("icon", "")),
            preinstalled_repos=list(data.get("preinstalled_repos") or []),
            default_voice=str(data.get("default_voice", "")),
            default_shell_access=bool(data.get("default_shell_access", False)),
        )

    # ------------------------------------------------------------------ io ---

    def save(self, filepath: str | Path) -> None:
        """Write the manifest, in the format the filename asks for.

        `.json` gets JSON; anything else gets YAML, which makes `.agentfleet.yaml`
        actually YAML rather than JSON wearing a `.yaml` extension.
        """
        path = Path(filepath)
        path.parent.mkdir(parents=True, exist_ok=True)
        text = (
            json.dumps(self.to_dict(), indent=2)
            if path.suffix.lower() == ".json"
            else dump_yaml(self.to_dict())
        )
        path.write_text(text, encoding="utf-8")

    @classmethod
    def load(cls, filepath: str | Path) -> "ArchetypeManifest":
        """Read a manifest, in whichever format it turns out to be.

        Sniffed rather than taken from the extension: the exporter has written
        both over the years, and refusing a `.yaml` file that contains JSON
        would reject manifests this very tool produced.
        """
        raw = Path(filepath).read_text(encoding="utf-8")
        return cls.from_dict(parse_manifest(raw))


def parse_manifest(raw: str) -> dict[str, Any]:
    """Parse a manifest that may be JSON or YAML."""
    stripped = raw.lstrip()
    if stripped.startswith("{"):
        return json.loads(raw)
    try:
        import yaml  # type: ignore

        return yaml.safe_load(raw) or {}
    except ImportError:
        return load_yaml(raw)


# --------------------------------------------------------------------- YAML ---
#
# A deliberately small subset: top-level `key: scalar`, `key:` followed by an
# indented block sequence of scalars, and `key:` followed by an indented mapping.
# Nested structures -- which `mcp_servers` and `recorded_skills` are -- are
# emitted as inline JSON, which is valid YAML because YAML is a superset of JSON.
# That keeps this honest: it never pretty-prints something it could not read
# back.


def dump_yaml(data: dict[str, Any]) -> str:
    lines: list[str] = []
    for key, value in data.items():
        if isinstance(value, dict):
            if not value:
                lines.append(f"{key}: {{}}")
                continue
            lines.append(f"{key}:")
            for k, v in value.items():
                lines.append(f"  {k}: {_scalar(v)}")
        elif isinstance(value, list):
            if not value:
                lines.append(f"{key}: []")
                continue
            # A list of scalars becomes a block sequence, which is the readable
            # form. A list of anything structured stays inline JSON.
            if all(isinstance(v, (str, int, float, bool)) for v in value):
                lines.append(f"{key}:")
                for v in value:
                    lines.append(f"  - {_scalar(v)}")
            else:
                lines.append(f"{key}: {json.dumps(value)}")
        else:
            lines.append(f"{key}: {_scalar(value)}")
    return "\n".join(lines) + "\n"


def _scalar(v: Any) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return str(v)
    if v is None:
        return "null"
    # Always quoted, via JSON. A system prompt runs to paragraphs and contains
    # colons, hashes and newlines, every one of which changes the meaning of an
    # unquoted YAML scalar.
    return json.dumps(str(v))


def load_yaml(raw: str) -> dict[str, Any]:
    """Read back what dump_yaml writes, without PyYAML."""
    out: dict[str, Any] = {}
    key: str | None = None
    seq: list[Any] | None = None
    mapping: dict[str, Any] | None = None

    def flush() -> None:
        nonlocal key, seq, mapping
        if key is None:
            return
        if seq is not None:
            out[key] = seq
        elif mapping is not None:
            out[key] = mapping
        key, seq, mapping = None, None, None

    for line in raw.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue

        if line.startswith(("  - ", "- ")) and key is not None:
            if seq is None:
                seq = []
            seq.append(_unscalar(line.split("- ", 1)[1].strip()))
            continue

        if line.startswith("  ") and key is not None and seq is None:
            k, _, v = line.strip().partition(":")
            if mapping is None:
                mapping = {}
            mapping[k.strip()] = _unscalar(v.strip())
            continue

        flush()
        k, sep, v = line.partition(":")
        if not sep:
            continue
        k = k.strip()
        v = v.strip()
        if v == "":
            # A block sequence or mapping follows on the indented lines below.
            key = k
            continue
        out[k] = _unscalar(v)
    flush()
    return out


def _unscalar(v: str) -> Any:
    if v in ("[]", "{}"):
        return [] if v == "[]" else {}
    if v == "true":
        return True
    if v == "false":
        return False
    if v == "null":
        return None
    if v.startswith(("[", "{", '"')):
        try:
            return json.loads(v)
        except json.JSONDecodeError:
            return v
    try:
        return int(v)
    except ValueError:
        pass
    try:
        return float(v)
    except ValueError:
        return v
