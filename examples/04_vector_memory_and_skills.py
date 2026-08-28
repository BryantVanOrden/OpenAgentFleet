#!/usr/bin/env python3
"""Example 4: Persistent Long-Term Episodic Vector Memory and Skill Synthesis."""

from agentfleet import FleetClient

def main():
    fleet = FleetClient("http://localhost:8080")

    print("🧠 Querying Fleet Episodic Vector Memory...")
    # Agents automatically query memory using the 'recall' action or through API:
    results = fleet._get("/api/tasks") or []
    print(f"✅ Found {len(results)} past task trajectories indexed across the fleet.")

    print("\n🔍 Probing AI Self-Refinement Engine...")
    skills = fleet._get("/api/skills") or []
    print(f"✅ Loaded {len(skills)} continually refined SKILL.md workflows.")

if __name__ == "__main__":
    main()
