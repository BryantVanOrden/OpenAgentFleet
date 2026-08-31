#!/usr/bin/env python3
"""Example 4: fleet episodic memory and recorded skills.

Agents write memory with the `remember` action — private to themselves by
default, fleet-wide with `"memory_scope": "fleet"` — and read it back with
`recall`. This inspects the shared pool from the outside.

Honesty note baked into the API: search uses real embeddings only when an
embedding-capable provider is configured (Ollama, OpenAI or Gemini). Otherwise
it falls back to a keyword index, and /api/memory/fleet says which one you
have rather than letting "semantic search" quietly mean word overlap.
"""

from agentfleet import FleetClient


def main():
    fleet = FleetClient("http://localhost:8080")
    fleet.login("you@example.com", "your-password")  # or FleetClient(token=...)

    # 1. The shared pool: what agents chose to tell the whole fleet, plus the
    #    auto-indexed summaries of completed tasks.
    mem = fleet._get("/api/memory/fleet")
    search = mem.get("search", {})
    print("🧠 Fleet episodic memory")
    if search.get("semantic"):
        print(f"   Search: real embeddings ({search.get('model')})")
    else:
        print(f"   Search: keyword index ({search.get('model')}) — configure an "
              "Ollama/OpenAI/Gemini provider for semantic recall")

    shared = mem.get("shared", [])
    tasks = mem.get("tasks", [])
    print(f"   Shared findings: {len(shared)} · auto-indexed task trajectories: {len(tasks)}")
    for m in shared[:3]:
        print(f"   • {m['title']}: {m['content'][:80]}")

    # 2. Recorded skills: demonstrations compiled into SKILL.md workflows that
    #    agents replay, and that refine themselves after successful runs.
    skills = fleet._get("/api/skills") or []
    print(f"\n📚 {len(skills)} recorded skill(s)")
    for s in skills[:5]:
        print(f"   • {s['name']} (v{s.get('version', 1)})")


if __name__ == "__main__":
    main()
