# AgentFleet Python SDK & `fleetctl` CLI

Official Python SDK and command-line management client for
[**AgentFleet**](https://github.com/BryantVanOrden/OpenAgentFleet) — the
self-hosted platform for autonomous computer-use agents, where every agent
gets its own disposable Linux desktop that streams live into your browser.

---

## 📦 Installation

```bash
pip install open-agent-fleet
```

That gives you the `agentfleet` package and the `fleetctl` CLI. To work from
source instead:

```bash
git clone https://github.com/BryantVanOrden/OpenAgentFleet.git
pip install -e OpenAgentFleet/sdk/python
```

The SDK is a client — it talks to an AgentFleet orchestrator. To run one,
see the [main README](https://github.com/BryantVanOrden/OpenAgentFleet#readme)
(`make up` is the whole quickstart).

---

## ⚡ Quickstart (Python SDK)

```python
from agentfleet import FleetClient

# Connect to your local or remote orchestrator
fleet = FleetClient("http://localhost:8080", token="your-api-token")

# 1. Deploy a specialized bot archetype
bot = fleet.deploy_bot("cyber_ops", name="security-auditor")
print(f"Spawned {bot.name} (ID: {bot.id})")

# 2. Run an autonomous goal
task = bot.run("Run SAST vulnerability scan and compile CVSS briefing", wait=True)
print(f"Task status: {task.state}")
print(f"Result: {task.result}")

# 3. Launch a collaborative multi-agent swarm
swarm = fleet.launch_swarm(
    name="Fintech Mobile Audit",
    mission="Full-Stack Dev + QA Tester + CyberSec PenTester collaborative sweep",
)

# 4. Pocket TTS Voice Co-Pilot
fleet.speak("All tasks completed successfully, operator.", voice="shadow")
```

---

## 🛠️ Command-Line Interface (`fleetctl`)

The `fleetctl` command-line utility provides full administrative control, live telemetry, and system diagnostics:

```bash
# 1. Configuration & Authentication
fleetctl config set-url http://localhost:8080
fleetctl login admin@example.com

# 2. Comprehensive Diagnostics & Health Check
fleetctl diagnostics

# 3. List and Inspect Instances
fleetctl list
fleetctl inspect <instance_id>

# 4. Deploy Bots
fleetctl deploy --archetype cyber_ops --name "nightly-scanner"

# 5. Execute Tasks & Stream Live Execution
fleetctl run <instance_id> "Refactor backend authentication and run unit tests" --wait

# 6. Multi-Agent Swarm Mission Control
fleetctl swarm list
fleetctl swarm launch --name "Security Sweep" --mission "Penetration test staging endpoints"
fleetctl swarm watch <swarm_id>

# 7. Real-Time Pocket TTS Voice
fleetctl voice list
fleetctl voice speak "System operational and standing by." --voice shadow

# 8. Webhook & Autopilot Sinks
fleetctl webhooks list
fleetctl webhooks trigger github-pr-sync --data '{"pr": 42}'
```
