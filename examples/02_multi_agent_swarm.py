#!/usr/bin/env python3
"""Example 2: a collaborative multi-agent swarm on a shared blackboard.

A swarm runs on real bots: every member names an instance that exists, and
creating the swarm starts a task on each of them. (An earlier version of this
example passed no members and relied on the server inventing a default team —
bots that existed on no fleet, so nothing ever ran. The API now refuses that.)

Needs a running stack (`make up`) and a configured model provider, since every
member starts real work the moment the swarm is created.
"""

from agentfleet import FleetClient


def main():
    fleet = FleetClient("http://localhost:8080")
    # fleet.login("you@example.com", "...")   # or FleetClient(token=...)

    # 1. The team has to exist before it can be assigned. Two small bots keep
    #    the example cheap; use bigger tiers for real missions.
    print("🚀 Provisioning the team (two micro bots)...")
    architect = fleet.deploy_bot(archetype_id="fullstack_dev", name="swarm-architect", tier="micro")
    auditor = fleet.deploy_bot(archetype_id="qa_ui_ux", name="swarm-auditor", tier="micro")
    print(f"   {architect.name} and {auditor.name} are up")

    # 2. Launch the mission. Each member is told the mission, its own role, and
    #    who its teammates are, then starts a real task immediately.
    swarm = fleet.launch_swarm(
        name="Login Page Review",
        mission="Architect: implement the login form fix. Auditor: verify "
        "accessibility and post an APPROVED/REJECTED review of the result.",
        members=[
            {"instance_id": architect.id, "role": "Lead Architect"},
            {"instance_id": auditor.id, "role": "QA Auditor"},
        ],
    )
    print(f"✅ Swarm {swarm.id} is {swarm.status} with {len(swarm.members)} members")

    # 3. The operator can post to the same blackboard the bots use.
    msg = swarm.broadcast(
        content="Begin Phase 1: Architect prepares the change for QA review.",
        from_bot="Mission Operator",
        to_bot="all",
        phase="planning",
    )
    print(f"📢 [{msg.phase.upper()}] {msg.from_bot} ➔ {msg.to_bot}: {msg.content}")

    print(
        "\nWatch it in Mission Control (http://localhost:8081). Artifacts the "
        "members publish are sent to the other members for peer review; the "
        "mission completes when every reviewer has approved."
    )


if __name__ == "__main__":
    main()
