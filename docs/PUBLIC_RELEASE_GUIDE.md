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

1. Navigate to: **[https://github.com/BryantVanOrden/AgentFleet/settings](https://github.com/BryantVanOrden/AgentFleet/settings)**
2. Scroll to the bottom to the **Danger Zone** section.
3. Click **Change repository visibility** $\rightarrow$ select **Make public**.
4. Confirm by entering `BryantVanOrden/AgentFleet`.

---

## 📦 3. Publishing Python SDK & `fleetctl` to PyPI

The Python SDK in [`sdk/python/`](../sdk/python/) is configured and ready for packaging:

### Step 1: Install Build Tools
```bash
pip install --upgrade build twine
```

### Step 2: Build Distribution Wheel & Source Tarball
```bash
cd sdk/python
python -m build
```
This generates `dist/agentfleet-0.1.0-py3-none-any.whl` and `dist/agentfleet-0.1.0.tar.gz`.

### Step 3: Test Upload to TestPyPI (Optional)
```bash
twine upload --repository testpypi dist/*
```

### Step 4: Official Production Release to PyPI
```bash
twine upload dist/*
```
Once uploaded, users worldwide can install with:
```bash
pip install agentfleet
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
- 🐝 Multi-Agent Swarm Mission Control
- 🎙️ Pocket TTS Real-Time Voice Co-Pilot (with 6 curated voices)
- 🧠 Long-Term Episodic Vector Memory
- 📱 Flutter iOS/Android Companion App
- 🐍 Python Client SDK & `fleetctl` CLI
