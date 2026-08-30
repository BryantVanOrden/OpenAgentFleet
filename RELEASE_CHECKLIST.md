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
2. **Push the tag.** `git tag -a v1.2.0 -m "AgentFleet v1.2.0" && git push
   origin v1.2.0`. The workflow triggers on `tags: [ 'v*' ]`, takes the version
   from the tag it was built for, and creates the release.

Two things about this workflow are still worth knowing:

- **Only a tag build publishes.** A push to `master` runs the tests and then
  exits the publish job with "not a tag build; nothing to release". This is
  deliberate — it is what stops every merge re-uploading assets — but it means
  a manual dispatch on a branch will not cut a release however long you wait.
- **The heavy builds run on every push to `master` once the variable is set.**
  `RUN_HEAVY_BUILDS=yes` is not scoped to releases. Turning it off again after a
  release is part of the job.

Release notes are written by hand into `docs/RELEASE_NOTES_<version>.md`, and
the workflow prefers that file when it exists; it falls back to
`--generate-notes`, which produces a commit list rather than prose.

> Earlier versions of this checklist said a tag triggered nothing and that the
> publish job hardcoded `v1.0.0`. Both were true and both are fixed; following
> the old workaround now would attach the wrong assets to the wrong release.

---

### 5. Publish container images to GHCR
```bash
# Build sandbox image
docker build -t ghcr.io/bryantvanorden/agentfleet-sandbox:latest ./sandbox

# Build orchestrator image
docker build -t ghcr.io/bryantvanorden/agentfleet-backend:latest ./backend

# Push images
docker push ghcr.io/bryantvanorden/agentfleet-sandbox:latest
docker push ghcr.io/bryantvanorden/agentfleet-backend:latest
```

---

### 6. Announcement channels
- **Hacker News**: Submit Show HN post highlighting self-hosted computer use, Pocket TTS voice co-pilot, and autonomous multi-bot swarms.
- **X / Twitter**: Post a short 30-second screen capture demonstration showing live task execution and demonstration teaching.
- **Reddit**: Share in `r/LocalLLaMA`, `r/MachineLearning`, and `r/Python`.
