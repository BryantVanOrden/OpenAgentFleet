# Contributing to OpenAgentFleet

Bug fixes, documentation improvements, performance work and new features are all
welcome.

---

## Cardinal rule: responsible security disclosure

> [!CAUTION]
> **DO NOT open public GitHub Issues or public Pull Requests for security vulnerabilities, sandbox breakout vectors, or credential leaks.**
> 
> Follow our [Security Policy](SECURITY.md) to report security issues privately via GitHub Security Advisories or by emailing `supermanismebvo123@gmail.com`.

---

## Pull request standards

### 1. PR notes and description
Your PR description must clearly answer:
* **What changed?**: A bulleted summary of specific modifications across backend, frontend, SDK, or sandbox.
* **Why?**: The problem or motivation behind this change (link related GitHub issues if applicable).
* **Testing Performed**: Exact commands executed and confirmation that automated tests passed.
* **Visual Evidence (Required for UI PRs)**: Before/After screenshots or a short GIF/video demonstrating changes to the React Admin Console (`admin/`) or Flutter Mobile App (`mobile/`).

### 2. Pre-PR verification

Run all five suites locally. The Go suite is the largest component and CI runs it
first, so a change that has not been through it will fail before anything else
is looked at.

```bash
# 1. Go orchestrator — the backend test suite
cd backend && go test ./...

# 2. Python sandbox daemon
cd ../sandbox/agentd && python -m unittest discover -p "test_*.py"

# 3. Python SDK and CLI
cd ../../sdk/python && python -m unittest discover -s tests -p "test_*.py"

# 4. React admin console build (must produce no TypeScript errors)
cd ../../admin && npm run build

# 5. Flutter companion app
cd ../mobile && flutter analyze && flutter test
```

`make test` runs all of these together (it used to run `flutter analyze` and
skip `flutter test` entirely, so nine mobile test files never ran in CI), but it needs Go, Python, Node and Flutter
all present; running them individually is easier to debug when one is missing.

Some backend store tests need a reachable Postgres and skip themselves without
one. A green run that skipped them is not a full run.

### 3. Git and commit message conventions
Follow standard [Conventional Commits](https://www.conventionalcommits.org/):
* `feat:` A new user-facing feature or API capability
* `fix:` A bug fix or runtime patch
* `docs:` Documentation, README, or guide updates
* `test:` Adding or refactoring test suites
* `refactor:` Code restructuring without changing external behavior
* `perf:` Performance improvements or memory optimizations

---

## Architecture and code standards

| Component Layer | Technology | Primary Principles |
| :--- | :--- | :--- |
| **Backend Orchestrator** | Go 1.25+ | Clean interfaces, explicit error handling, race-condition safety with `sync.RWMutex`, zero plaintext credential leakage. |
| **Sandbox Engine** | Python 3.10+ | Strict cgroup isolation, Set-of-Marks visual coordinate transformation, fallback-safe imports. |
| **Python SDK & CLI** | Python 3.10+ | Zero external dependencies outside standard library (`urllib.request`), UTF-8 terminal safety across Windows/Linux/macOS. |
| **Admin Console** | React 19 + TypeScript + Vite | Strict TypeScript typing, responsive dark theme styling with Tailwind CSS, clean API abstraction. |
| **Mobile Companion** | Flutter 3.24+ / Dart | Riverpod state management, cross-platform responsiveness (Android, iOS, Windows, Linux). |

---

## Licensing notice

By contributing to OpenAgentFleet, you agree that your contributions will be licensed under the **[MIT License](LICENSE)**.
