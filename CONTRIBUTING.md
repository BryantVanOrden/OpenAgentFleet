# Contributing to AgentFleet 🤝

Thank you for your interest in contributing to **AgentFleet**! We welcome bug fixes, documentation improvements, performance optimizations, and new features from the community.

---

## 🚨 Cardinal Rule: Responsible Security Disclosure

> [!CAUTION]
> **DO NOT open public GitHub Issues or public Pull Requests for security vulnerabilities, sandbox breakout vectors, or credential leaks.**
> 
> Follow our [Security Policy](SECURITY.md) to report security issues privately via GitHub Security Advisories or by emailing `supermanismebvo123@gmail.com`.

---

## 📜 Pull Request (PR) Quality Standards

To ensure fast reviews and high software quality, every Pull Request must meet these standards:

### 1. 📝 High-Quality PR Notes & Description
Your PR description must clearly answer:
* **What changed?**: A bulleted summary of specific modifications across backend, frontend, SDK, or sandbox.
* **Why?**: The problem or motivation behind this change (link related GitHub issues if applicable).
* **Testing Performed**: Exact commands executed and confirmation that automated tests passed.
* **Visual Evidence (Required for UI PRs)**: Before/After screenshots or a short GIF/video demonstrating changes to the React Admin Console (`admin/`) or Flutter Mobile App (`mobile/`).

### 2. 🧪 Pre-PR Verification Checklist
Before submitting a PR, verify that all test suites pass locally:

```bash
# 1. Test Python Sandbox Daemon (66 tests)
cd sandbox/agentd && python -m unittest discover -p "test_*.py"

# 2. Test Python SDK & CLI (7 tests)
cd ../../sdk/python && python -m unittest discover -s tests -p "test_*.py"

# 3. Verify React Admin Console Build (0 TypeScript errors)
cd ../../admin && npm run build

# 4. Verify Flutter Companion App (0 warnings / errors)
cd ../mobile && flutter analyze
```

### 3. 🌿 Git & Commit Message Conventions
Follow standard [Conventional Commits](https://www.conventionalcommits.org/):
* `feat:` A new user-facing feature or API capability
* `fix:` A bug fix or runtime patch
* `docs:` Documentation, README, or guide updates
* `test:` Adding or refactoring test suites
* `refactor:` Code restructuring without changing external behavior
* `perf:` Performance improvements or memory optimizations

---

## 🛠️ Architecture & Code Standards

| Component Layer | Technology | Primary Principles |
| :--- | :--- | :--- |
| **Backend Orchestrator** | Go 1.23+ | Clean interfaces, explicit error handling, race-condition safety with `sync.RWMutex`, zero plaintext credential leakage. |
| **Sandbox Engine** | Python 3.10+ | Strict cgroup isolation, Set-of-Marks visual coordinate transformation, fallback-safe imports. |
| **Python SDK & CLI** | Python 3.10+ | Zero external dependencies outside standard library (`urllib.request`), UTF-8 terminal safety across Windows/Linux/macOS. |
| **Admin Console** | React 19 + TypeScript + Vite | Strict TypeScript typing, responsive dark theme styling with Tailwind CSS, clean API abstraction. |
| **Mobile Companion** | Flutter 3.24+ / Dart | Riverpod state management, cross-platform responsiveness (Android, iOS, Windows, Linux). |

---

## ⚖️ Licensing Notice

By contributing to AgentFleet, you agree that your contributions will be licensed under the **[PolyForm Noncommercial License 1.0.0](LICENSE)**.
