# AgentFleet Official Public Release Checklist 🚀

When you are ready to transition AgentFleet from private development to an official open-source release, follow this step-by-step checklist.

---

### 1. 🔒 Security & Secrets Hygiene Audit
- [ ] Confirm no live API keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, etc.) or custom tokens are hardcoded in `.env`, tests, or commits.
- [ ] Verify `.gitignore` contains all secrets, logs, test artifacts, and build outputs (`dist/`, `build/`, `.dart_tool/`, `*.tar.gz`).
- [ ] Verify database migrations (`backend/internal/store/migrations/`) execute cleanly on a fresh database.

---

### 2. 🌐 Make Repository Public on GitHub
1. Go to **GitHub Repository Settings**: `https://github.com/BryantVanOrden/AgentFleet/settings`
2. Scroll to the **Danger Zone** at the bottom.
3. Click **Change repository visibility** ➔ Select **Make public**.
4. Confirm by typing `BryantVanOrden/AgentFleet`.

---

### 3. 📦 Publish Official Python SDK to PyPI (`pip install agentfleet`)
```bash
cd sdk/python

# 1. Install build tools
pip install --upgrade build twine

# 2. Build sdist and wheel packages
python -m build

# 3. Upload to PyPI (requires PyPI API Token)
python -m twine upload dist/*
```

---

### 4. 🚀 Trigger Automated Binary Releases (CI/CD)
To build production Linux desktop, Windows `.exe`, and Android APK binaries:
1. In GitHub Repository Settings ➔ **Secrets and variables** ➔ **Actions**:
   - Set repository variable `RUN_RELEASE_BUILDS` to `yes`.
2. Create and push a version tag:
   ```bash
   git tag -a v1.0.0 -m "AgentFleet v1.0.0 Public Release"
   git push origin v1.0.0
   ```
3. GitHub Actions (`.github/workflows/ci.yml`) will automatically build, package, and attach the binaries to the GitHub Release.

---

### 5. 🐳 Publish Container Images to Docker Hub / GitHub Packages (GHCR)
```bash
# Build sandbox image
docker build -t ghcr.io/bryantvanorden/agentfleet-sandbox:latest -f sandbox/Dockerfile .

# Build orchestrator image
docker build -t ghcr.io/bryantvanorden/agentfleet-backend:latest -f backend/Dockerfile .

# Push images
docker push ghcr.io/bryantvanorden/agentfleet-sandbox:latest
docker push ghcr.io/bryantvanorden/agentfleet-backend:latest
```

---

### 6. 📢 Public Announcement Channels
- **Hacker News**: Submit Show HN post highlighting self-hosted computer use, Pocket TTS voice co-pilot, and autonomous multi-bot swarms.
- **X / Twitter**: Post a short 30-second screen capture demonstration showing live task execution and demonstration teaching.
- **Reddit**: Share in `r/LocalLLaMA`, `r/MachineLearning`, and `r/Python`.
