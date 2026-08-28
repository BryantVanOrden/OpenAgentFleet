#!/usr/bin/env python3
"""Example 2: Launch a collaborative Multi-Agent Team Swarm operating on a shared blackboard."""

from agentfleet import FleetClient

def main():
    fleet = FleetClient("http://localhost:8080")

    mission_name = "Fintech Mobile App Security & Accessibility Sweep"
    mission_goal = "Lead Architect writes code -> QA Auditor verifies accessibility -> CyberSec PenTester audits endpoints -> Deliverables posted to blackboard."

    print(f"🚀 Launching Collaborative Swarm: '{mission_name}'")
    swarm = fleet.launch_swarm(name=mission_name, mission=mission_goal)
    print(f"✅ Swarm ID: {swarm.id} (Status: {swarm.status})")
    print(f"👥 Assigned Members: {len(swarm.members)} specialized bots")

    # Broadcast an operator directive to the swarm blackboard
    print("\n📢 Broadcasting initial directive to swarm blackboard...")
    msg = swarm.broadcast(
        content="Begin Phase 1: Lead Architect prepare endpoints for QA audit.",
        from_bot="Mission Operator",
        to_bot="all",
        phase="planning",
    )
    print(f"Message Posted: [{msg.phase.upper()}] {msg.from_bot} ➔ {msg.to_bot}: {msg.content}")

if __name__ == "__main__":
    main()
