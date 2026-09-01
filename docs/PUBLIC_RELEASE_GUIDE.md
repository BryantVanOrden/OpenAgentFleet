# 🚀 AgentFleet Official Public Release Guide

This document outlines the step-by-step checklist and procedures for taking **AgentFleet** public on GitHub and publishing the official Python SDK and `fleetctl` CLI to PyPI.

---

## 🔒 1. Pre-Flight Security & Secret Sanitation Audit

Before toggling repository visibility to Public:

- [x] **No hardcoded credentials**: All secret tokens, JWT secrets, and database passwords use environment variables with fallbacks (`${JWT_SECRET:-}`, `${DATABASE_URL:-}`).
- [x] **Vault Encryption**: Dynamic credentials use AES-256-GCM sealed keyrings stored in disposable tmpfs mounts.
- [x] **`.gitignore` Enforcement**: Verified `.env`, `db-data`, `artifacts/`, and `*.log` are gitignored.

---

## 🌐 2. Changing Repository Visibility on GitHub

When you are ready to make the repository public:

1. Navigate to: **[https://github.com/BryantVanOrden/OpenAgentFleet/settings](https://github.com/BryantVanOrden/OpenAgentFleet/settings)**
2. Scroll to the bottom to the **Danger Zone** section.
3. Click **Change repository visibility** $\rightarrow$ select **Make public**.
4. Confirm by entering `BryantVanOrden/OpenAgentFleet`.

---

## 📦 3. Publishing Python SDK & `fleetctl` to PyPI

The Python SDK in [`sdk/python/`](../sdk/python/) publishes itself through
**PyPI Trusted Publishing** — the OIDC scheme where PyPI trusts a specific
GitHub Actions workflow instead of an API token, so there is no token to
create, store, rotate or leak.

### Step 1: Register the pending publisher on pypi.org (one time)
Log in to pypi.org, then **Your account → Publishing → Add a new pending
publisher** with exactly:

| Field | Value |
|---|---|
| PyPI project name | `open-agent-fleet` |
| Owner | `BryantVanOrden` |
| Repository name | `AgentFleet` |
| Workflow name | `publish-pypi.yml` |
| Environment name | `pypi` |

This *claims* the `open-agent-fleet` name: the first publish from that workflow
creates the project and you become its owner.

### Step 2: Create the GitHub environment (one time)
**Repo → Settings → Environments → New environment** named `pypi`. Optionally
add yourself as a required reviewer so every publish needs a click of approval.

### Step 3: Publish
Publishing a GitHub release runs
[`.github/workflows/publish-pypi.yml`](../.github/workflows/publish-pypi.yml)
automatically; it builds the sdist and wheel, runs `twine check`, installs the
wheel and runs the SDK test suite against the exact artifact, then uploads.
It can also be run by hand from the **Actions** tab (workflow_dispatch).

Once uploaded, users worldwide can install with:
```bash
pip install open-agent-fleet
```
And immediately run:
```bash
fleetctl --help
```

---

## ⚙️ 4. GitHub Actions Release Automation

In GitHub Repository Settings $\rightarrow$ **Secrets and variables** $\rightarrow$ **Actions** $\rightarrow$ **Variables**:

- To run standard rapid CI (fast validation of Go backend, Python sandbox, and React frontend), leave default settings.
- To enable heavy multi-platform builds (**Flutter Android APK**, **Linux Desktop Binary**, **Windows `.exe`**), add repository variable:
  - **Name**: `RUN_HEAVY_BUILDS`
  - **Value**: `yes`

Or trigger the workflow manually at any time under **Actions** $\rightarrow$ **AgentFleet CI & Release Build Pipeline** $\rightarrow$ **Run workflow** (checking *Force heavy cross-platform builds*).

---

## 🌟 5. Launch Announcement & Community Links

Once public, you can feature:
- 📖 [Official Documentation & Architecture](../README.md)
- 🐝 Multi-agent swarm mission control, with peer review of what agents produce
- 🎙️ Pocket TTS voice, with six distinct speakers
- 🧠 Long-term episodic memory, shared across the fleet
- 🔌 Model Context Protocol client (stdio and Streamable HTTP)
- ⛓️ Parallel DAG pipelines with conditional branches
- 📱 Flutter companion app on five platforms
- 🐍 Python client SDK & `fleetctl` CLI

Two honesty notes for whoever writes the announcement, because both have a
specific caveat that is easy to overstate:

- **Memory** is semantic by default: a local embedding sidecar ships in the
  compose stack, and configured providers that can embed (Ollama, OpenAI,
  Gemini) are preferred over it for quality. "Vector memory" is now a fair
  description; `/api/memory/fleet` still reports the live scheme, and only a
  deployment that removes the sidecar *and* has no embedding provider falls
  back to the keyword index.
- **The README's "Design limits, stated plainly"** section is the list to
  check before claiming anything. It is kept current deliberately; the
  announcement should not contradict it.
