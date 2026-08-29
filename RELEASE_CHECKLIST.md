# Release checklist

Steps for taking AgentFleet from private development to a public release, and for
each release after that.

---

### 1. Security and secrets hygiene
- [ ] Confirm no live API keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, etc.) or custom tokens are hardcoded in `.env`, tests, or commits.
- [ ] Verify `.gitignore` contains all secrets, logs, test artifacts, and build outputs (`dist/`, `build/`, `.dart_tool/`, `*.tar.gz`).
- [ ] Verify database migrations (`backend/internal/store/migrations/`) execute cleanly on a fresh database.

---

### 2. Make the repository public
1. Go to **GitHub Repository Settings**: `https://github.com/BryantVanOrden/AgentFleet/settings`
2. Scroll to the **Danger Zone** at the bottom.
3. Click **Change repository visibility** ➔ Select **Make public**.
4. Confirm by typing `BryantVanOrden/AgentFleet`.

---

### 3. Publish the Python SDK to PyPI
The SDK is not on PyPI yet, so `pip install agentfleet` does not work. Until this
step is done, the README and the quickstart scripts install it from the repo with
`pip install -e ./sdk/python`.

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

### 4. Build the binaries

To build the Linux desktop bundle, the Windows `.exe` and the Android APK:

1. In **Settings → Secrets and variables → Actions**, set the repository
   **variable** `RUN_HEAVY_BUILDS` to `yes`. (Earlier revisions of this checklist
   named a variable `RUN_RELEASE_BUILDS`. Nothing reads that; the workflow reads
   `RUN_HEAVY_BUILDS`.) Alternatively, run the workflow manually from the Actions
   tab with **Force heavy cross-platform builds** ticked, which sets the same
   condition for one run without leaving it on for every push.
2. Push to `master`, or dispatch the workflow manually.

Three things about this workflow are not what you would expect, and all three
will bite on a second release:

- **Pushing a tag does nothing.** `.github/workflows/ci.yml` triggers on push to
  `master`/`main`, on pull requests, and on manual dispatch. It has no `tags:`
  filter, so `git push origin v1.1.0` runs no workflow at all.
- **The publish job hardcodes `TAG="v1.0.0"`.** The `publish-release` job creates
  or uploads to the `v1.0.0` release, whatever version you are actually shipping.
  Until that is parameterised, assets for any later version will land on the
  v1.0.0 release. Either edit the job before running it, or download the
  artefacts from the workflow run and attach them to the right release by hand.
- **The heavy builds run on every push to `master` once the variable is set.**
  `RUN_HEAVY_BUILDS=yes` is not scoped to releases. Turning it off again after a
  release is part of the job.

Release notes are written by hand into `docs/RELEASE_NOTES_<version>.md` and
`CHANGELOG.md`; the workflow's `--generate-notes` produces a commit list, not
those.

---

### 5. Publish container images to GHCR
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

### 6. Announcement channels
- **Hacker News**: Submit Show HN post highlighting self-hosted computer use, Pocket TTS voice co-pilot, and autonomous multi-bot swarms.
- **X / Twitter**: Post a short 30-second screen capture demonstration showing live task execution and demonstration teaching.
- **Reddit**: Share in `r/LocalLLaMA`, `r/MachineLearning`, and `r/Python`.
