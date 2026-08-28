#!/usr/bin/env python3
"""Example 1: Deploy a specialized Bot Archetype and execute an autonomous task."""

from agentfleet import FleetClient

def main():
    # 1. Connect to AgentFleet orchestrator
    fleet = FleetClient("http://localhost:8080")

    print("🚀 Deploying a specialized Full-Stack Developer bot...")
    bot = fleet.deploy_bot(
        archetype_id="fullstack_dev",
        name="lead-architect-01",
        tier="standard",
    )
    print(f"✅ Spawned {bot.name} (Instance ID: {bot.id})")

    # 2. Dispatch an autonomous goal and stream steps
    goal = "Create a modern REST API server in Python using FastAPI with /healthz and /items endpoints, and write unit tests."
    print(f"\n🎯 Dispatching goal: '{goal}'")
    task = bot.run(goal, max_steps=40, wait=True)

    print("\n------------------------------------------------------------")
    print(f"Task State:  {task.state.upper()}")
    print(f"Task Result: {task.result}")

if __name__ == "__main__":
    main()
