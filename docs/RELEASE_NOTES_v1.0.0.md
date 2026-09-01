# AgentFleet 1.0.0

The first public release.

AgentFleet is a self-hosted platform for autonomous computer-use agents:
every agent gets its own disposable Linux desktop that streams live into your
browser — click the stream and you are driving; let go and the agent carries
on. It runs on your hardware, against your models, and pings your phone when
an agent needs a human.

## Highlights

- **Real desktops, two isolation tiers.** A hardened container by default, or
  a real virtual machine with `"driver": "qemu"` — the guest disk is converted
  from the container image at build time, so the VM runs byte-for-byte the
  same stack and cannot drift. KVM when the host has it; software emulation
  otherwise. Egress policies on the VM tier are enforced in the runner's
  network namespace — where even a root agent inside the guest cannot flush
  them.
- **Watch, take over, teach.** Live noVNC streams, interactive takeover, and
  record-by-demonstration: do a task once and it compiles into an editable
  `SKILL.md` the agent follows and refines.
- **Local-first models.** Point it at Ollama on your own GPU and nothing
  leaves the building; OpenAI, Anthropic, Gemini and OpenAI-compatible
  gateways plug in behind the same interface with fallback chains and
  role-based model combinations.
- **The demo is real.** The README's hero is an uncut recording of a local
  vision model driving Firefox on a shell-disabled desktop — sixteen GUI
  actions, a real result. The real-time recording is attached to this release.
- **MCP, fully spoken.** Tools, resources and prompts over stdio and
  Streamable HTTP, with catalogues that refresh on the server's own
  announcement.
- **Fleet collaboration.** Durable peer messaging, a shared work catalog,
  swarms with an enforced planning barrier and peer review, and parallel DAG
  pipelines with conditional branches whose runs survive a restart.
- **Semantic memory by default.** A local embedding sidecar ships in the
  stack, so even a fleet with no embedding-capable provider gets semantic
  recall; `/api/memory/fleet` reports the live scheme.
- **Honest cost telemetry.** Live prices fetched daily with an offline
  fallback and the source reported; cached tokens counted and discounted.
- **Webhooks that know their senders.** GitHub, Stripe, HubSpot and
  Salesforce with their real signature schemes — including the SOAP Ack
  Salesforce requires.
- **Everything else**: six-voice TTS shared by agents, console and phone; a
  Python SDK and `fleetctl` CLI; portable archetype packages; organisations,
  departments and per-bot permissions; API keys; a Flutter companion app on
  five platforms.

## The honesty contract

The README carries a section called *Design limits, stated plainly*. It says
what each capability does not do — the VM tier has no GPU passthrough yet,
memory search is an exact scan sized (and benchmarked) to its working set,
and so on. Keeping that section truthful outranks keeping it short; if you
catch the docs claiming something the code does not do, that is the bug we
most want filed.

## Getting started

```bash
git clone https://github.com/BryantVanOrden/OpenAgentFleet.git
cd OpenAgentFleet
make up
```

Console on `:8081`, create your admin on the first-run screen, and point an
engine at your Ollama. The [README](https://github.com/BryantVanOrden/OpenAgentFleet#readme) covers the rest.
